// Package backupschedule declares owner-selected backup triggers. Rendering and
// Pulumi mocks do not qualify task identity, private networking or trigger delivery.
package backupschedule

import (
	"encoding/json"
	"errors"
	"net"
	"path"
	"regexp"
	"strconv"
	"strings"

	aws "github.com/pulumi/pulumi-aws/sdk/v7/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/scheduler"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

var ErrInvalid = errors.New("invalid backup scheduling declaration")
var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var resourceIdentifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)
var sha256Value = regexp.MustCompile(`^[a-f0-9]{64}$`)
var absolutePath = regexp.MustCompile(`^/[A-Za-z0-9_./-]+$`)
var image = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*@sha256:[a-f0-9]{64}$`)
var rolePath = regexp.MustCompile(`^[A-Za-z0-9_+=,.@/-]{1,576}$`)
var subnet = regexp.MustCompile(`^subnet-(?:[a-f0-9]{8}|[a-f0-9]{17})$`)
var securityGroup = regexp.MustCompile(`^sg-(?:[a-f0-9]{8}|[a-f0-9]{17})$`)
var account = regexp.MustCompile(`^[0-9]{12}$`)
var region = regexp.MustCompile(`^[a-z]{2}(?:-[a-z]+)+-[0-9]+$`)
var environmentObject = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]/[A-Za-z0-9_./-]+\.env$`)

// TaskAssets are declarations about an existing immutable task definition. Owner
// provider qualification must verify its actual image, entrypoint and file access.
type TaskAssets struct {
	TaskDefinitionARN    string `json:"task_definition_arn"`
	ContainerName        string `json:"container_name"`
	ExpectedImage        string `json:"expected_image"`
	Executable           string `json:"executable"`
	ConfigFile           string `json:"config_file"`
	ExpectedConfigSHA256 string `json:"expected_config_sha256"`
	TaskRoleARN          string `json:"task_role_arn"`
	ExecutionRoleARN     string `json:"execution_role_arn"`
	EnvironmentFileARN   string `json:"environment_file_arn"`
}
type ManagedAssets struct {
	ClusterARN            string     `json:"cluster_arn"`
	ScheduleGroupARN      string     `json:"schedule_group_arn"`
	RunScheduleName       string     `json:"run_schedule_name"`
	CheckScheduleName     string     `json:"check_schedule_name"`
	RunSchedulerRoleARN   string     `json:"run_scheduler_role_arn"`
	CheckSchedulerRoleARN string     `json:"check_scheduler_role_arn"`
	SubnetIDs             []string   `json:"subnet_ids"`
	SecurityGroupIDs      []string   `json:"security_group_ids"`
	Run                   TaskAssets `json:"run"`
	Check                 TaskAssets `json:"check"`
}
type arn struct{ partition, service, region, account, resource string }

func parseARN(s string) (arn, bool) {
	p := strings.SplitN(s, ":", 6)
	if len(s) > 2048 || len(p) != 6 || p[0] != "arn" || (p[1] != "aws" && p[1] != "aws-us-gov" && p[1] != "aws-cn") {
		return arn{}, false
	}
	return arn{p[1], p[2], p[3], p[4], p[5]}, true
}
func PathValid(s string) bool {
	return len(s) <= 512 && s != "/" && absolutePath.MatchString(s) && path.Clean(s) == s
}
func SHA256Valid(s string) bool { return sha256Value.MatchString(s) }
func ImageValid(s string) bool {
	if len(s) > 512 || !image.MatchString(s) {
		return false
	}
	p := strings.SplitN(s, "@", 2)[0]
	return !strings.Contains(path.Base(p), ":") && !strings.Contains(p, "//") && path.Clean(p) == p
}
func roleARN(s string, scope arn) bool {
	a, ok := parseARN(s)
	if !ok || a.partition != scope.partition || a.service != "iam" || a.region != "" || a.account != scope.account || !strings.HasPrefix(a.resource, "role/") {
		return false
	}
	p := strings.TrimPrefix(a.resource, "role/")
	return rolePath.MatchString(p) && len(path.Base(p)) <= 64 && path.Clean(p) == p && !strings.HasPrefix(p, "/")
}
func distinctIDs(ids []string, max int, re *regexp.Regexp) bool {
	if len(ids) == 0 || len(ids) > max {
		return false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !re.MatchString(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func taskValid(t TaskAssets, scope arn) bool {
	a, ok := parseARN(t.TaskDefinitionARN)
	if !ok || a.partition != scope.partition || a.account != scope.account || a.region != scope.region || a.service != "ecs" || !strings.HasPrefix(a.resource, "task-definition/") {
		return false
	}
	f := strings.Split(strings.TrimPrefix(a.resource, "task-definition/"), ":")
	if len(f) != 2 || !resourceIdentifier.MatchString(f[0]) {
		return false
	}
	n, e := strconv.ParseUint(f[1], 10, 64)
	if e != nil || n == 0 || strconv.FormatUint(n, 10) != f[1] {
		return false
	}
	env, ok := parseARN(t.EnvironmentFileARN)
	if !ok || !environmentBucketValid(strings.SplitN(env.resource, "/", 2)[0]) || env.partition != scope.partition || env.service != "s3" || env.region != "" || env.account != "" || !environmentObject.MatchString(env.resource) || path.Clean(env.resource) != env.resource {
		return false
	}
	return identifier.MatchString(t.ContainerName) && PathValid(t.Executable) && PathValid(t.ConfigFile) && SHA256Valid(t.ExpectedConfigSHA256) && ImageValid(t.ExpectedImage) && roleARN(t.TaskRoleARN, scope) && roleARN(t.ExecutionRoleARN, scope) && t.TaskRoleARN != t.ExecutionRoleARN
}

func environmentBucketValid(s string) bool {
	if len(s) < 3 || len(s) > 63 || net.ParseIP(s) != nil || strings.Contains(s, "..") || strings.Contains(s, ".-") || strings.Contains(s, "-.") || strings.HasPrefix(s, "xn--") || strings.HasSuffix(s, "-s3alias") || strings.HasSuffix(s, "--ol-s3") || strings.HasSuffix(s, ".mrap") || strings.HasSuffix(s, "--x-s3") || strings.HasSuffix(s, "--table-s3") {
		return false
	}
	return true
}

// Validate checks declarations only; it never inspects task definitions or AWS.
func Validate(c ManagedAssets) error {
	s, ok := parseARN(c.ClusterARN)
	if !ok || s.service != "ecs" || !account.MatchString(s.account) || !region.MatchString(s.region) || !strings.HasPrefix(s.resource, "cluster/") || !resourceIdentifier.MatchString(strings.TrimPrefix(s.resource, "cluster/")) {
		return ErrInvalid
	}
	g, ok := parseARN(c.ScheduleGroupARN)
	if !ok || g.partition != s.partition || g.account != s.account || g.region != s.region || g.service != "scheduler" || !strings.HasPrefix(g.resource, "schedule-group/") || !identifier.MatchString(strings.TrimPrefix(g.resource, "schedule-group/")) {
		return ErrInvalid
	}
	if !identifier.MatchString(c.RunScheduleName) || !identifier.MatchString(c.CheckScheduleName) || c.RunScheduleName == c.CheckScheduleName || !distinctIDs(c.SubnetIDs, 16, subnet) || !distinctIDs(c.SecurityGroupIDs, 5, securityGroup) || !roleARN(c.RunSchedulerRoleARN, s) || !roleARN(c.CheckSchedulerRoleARN, s) || !taskValid(c.Run, s) || !taskValid(c.Check, s) || c.Run.TaskDefinitionARN == c.Check.TaskDefinitionARN || c.Run.EnvironmentFileARN == c.Check.EnvironmentFileARN {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, r := range []string{c.RunSchedulerRoleARN, c.CheckSchedulerRoleARN, c.Run.TaskRoleARN, c.Check.TaskRoleARN} {
		if seen[r] {
			return ErrInvalid
		}
		seen[r] = true
	}
	if seen[c.Run.ExecutionRoleARN] || seen[c.Check.ExecutionRoleARN] {
		return ErrInvalid
	}
	return nil
}

type overrideFile struct {
	Value string `json:"value"`
	Type  string `json:"type"`
}
type overrideContainer struct {
	Name             string         `json:"name"`
	Command          []string       `json:"command"`
	EnvironmentFiles []overrideFile `json:"environmentFiles"`
}
type overrides struct {
	ContainerOverrides []overrideContainer `json:"containerOverrides"`
}

// Override returns a bounded ECS task override with no credential values.
func Override(t TaskAssets, command string) (string, error) {
	scope, valid := parseARN(t.TaskDefinitionARN)
	if !valid || !account.MatchString(scope.account) || !region.MatchString(scope.region) || !taskValid(t, scope) || (command != "run" && command != "check") {
		return "", ErrInvalid
	}
	body, e := json.Marshal(overrides{[]overrideContainer{{t.ContainerName, []string{"backup", command, "--config", t.ConfigFile}, []overrideFile{{t.EnvironmentFileARN, "s3"}}}}})
	if e != nil || len(body) > 8192 {
		return "", ErrInvalid
	}
	for _, b := range body {
		if b > 127 {
			return "", ErrInvalid
		}
	}
	return string(body), nil
}

type Declaration struct{ ScopeAccount, ScopeRegion, RunPolicy, CheckPolicy, RunTrust, CheckTrust string }

func policy(cluster string, t TaskAssets) string {
	b, _ := json.Marshal(map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{"Effect": "Allow", "Action": "ecs:RunTask", "Resource": t.TaskDefinitionARN, "Condition": map[string]any{"ArnEquals": map[string]string{"ecs:cluster": cluster}}}, map[string]any{"Effect": "Allow", "Action": "iam:PassRole", "Resource": []string{t.TaskRoleARN, t.ExecutionRoleARN}, "Condition": map[string]any{"StringEquals": map[string]string{"iam:PassedToService": "ecs-tasks.amazonaws.com"}}}}})
	return string(b)
}
func trust(s arn, group string) string {
	principal := "scheduler.amazonaws.com"
	b, _ := json.Marshal(map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{"Effect": "Allow", "Action": "sts:AssumeRole", "Principal": map[string]string{"Service": principal}, "Condition": map[string]any{"StringEquals": map[string]string{"aws:SourceAccount": s.account}, "ArnEquals": map[string]string{"aws:SourceArn": "arn:" + s.partition + ":scheduler:" + s.region + ":" + s.account + ":schedule-group/" + group}}}}})
	return string(b)
}
func Describe(c ManagedAssets) (Declaration, error) {
	if Validate(c) != nil {
		return Declaration{}, ErrInvalid
	}
	if _, e := Override(c.Run, "run"); e != nil {
		return Declaration{}, e
	}
	if _, e := Override(c.Check, "check"); e != nil {
		return Declaration{}, e
	}
	s, _ := parseARN(c.ClusterARN)
	g, _ := parseARN(c.ScheduleGroupARN)
	group := strings.TrimPrefix(g.resource, "schedule-group/")
	return Declaration{s.account, s.region, policy(c.ClusterARN, c.Run), policy(c.ClusterARN, c.Check), trust(s, group), trust(s, group)}, nil
}

type Component struct {
	pulumi.ResourceState
	Run, Check    *scheduler.Schedule
	ExpectedTrust Declaration
}

// New requires an explicit owner-selected provider and verifies its account and
// region before registering resources. It adds two schedules and their exact
// inline invocation policies to the selected existing scheduler roles. It does
// not create roles or repair trust; the owner must qualify/apply ExpectedTrust.
func New(ctx *pulumi.Context, name string, c ManagedAssets, activated bool, provider *aws.Provider, opts ...pulumi.ResourceOption) (*Component, error) {
	d, e := Describe(c)
	if e != nil || ctx == nil || provider == nil || !identifier.MatchString(name) {
		return nil, ErrInvalid
	}
	who, e := aws.GetCallerIdentity(ctx, nil, pulumi.Provider(provider))
	if e != nil || who.AccountId != d.ScopeAccount {
		return nil, ErrInvalid
	}
	where, e := aws.GetRegion(ctx, nil, pulumi.Provider(provider))
	if e != nil || where.Region != d.ScopeRegion {
		return nil, ErrInvalid
	}
	result := &Component{ExpectedTrust: d}
	base := append([]pulumi.ResourceOption{}, opts...)
	base = append(base, pulumi.Provider(provider))
	if e = ctx.RegisterComponentResource("amos:backup:Scheduling", name, result, base...); e != nil {
		return nil, e
	}
	child := []pulumi.ResourceOption{pulumi.Parent(result), pulumi.Provider(provider)}
	g, _ := parseARN(c.ScheduleGroupARN)
	group := strings.TrimPrefix(g.resource, "schedule-group/")
	state := "DISABLED"
	if activated {
		state = "ENABLED"
	}
	for i, t := range []TaskAssets{c.Run, c.Check} {
		command, scheduleName, role, permission := "run", c.RunScheduleName, c.RunSchedulerRoleARN, d.RunPolicy
		if i == 1 {
			command, scheduleName, role, permission = "check", c.CheckScheduleName, c.CheckSchedulerRoleARN, d.CheckPolicy
		}
		roleID, _ := parseARN(role)
		invokePolicy, e := iam.NewRolePolicy(ctx, name+"-"+command+"-invoke", &iam.RolePolicyArgs{Role: pulumi.String(path.Base(roleID.resource)), Policy: pulumi.String(permission)}, child...)
		if e != nil {
			return nil, e
		}
		input, _ := Override(t, command)
		scheduleOpts := append(append([]pulumi.ResourceOption{}, child...), pulumi.DependsOn([]pulumi.Resource{invokePolicy}))
		scheduled, e := scheduler.NewSchedule(ctx, name+"-"+command, &scheduler.ScheduleArgs{Name: pulumi.String(scheduleName), GroupName: pulumi.String(group), Region: pulumi.String(d.ScopeRegion), State: pulumi.String(state), ScheduleExpression: pulumi.String("rate(1 minute)"), ScheduleExpressionTimezone: pulumi.String("UTC"), FlexibleTimeWindow: &scheduler.ScheduleFlexibleTimeWindowArgs{Mode: pulumi.String("OFF")}, Target: &scheduler.ScheduleTargetArgs{Arn: pulumi.String(c.ClusterARN), RoleArn: pulumi.String(role), Input: pulumi.String(input), RetryPolicy: &scheduler.ScheduleTargetRetryPolicyArgs{MaximumEventAgeInSeconds: pulumi.Int(60), MaximumRetryAttempts: pulumi.Int(0)}, EcsParameters: &scheduler.ScheduleTargetEcsParametersArgs{TaskDefinitionArn: pulumi.String(t.TaskDefinitionARN), TaskCount: pulumi.Int(1), LaunchType: pulumi.String("FARGATE"), PlatformVersion: pulumi.String("1.4.0"), EnableExecuteCommand: pulumi.Bool(false), NetworkConfiguration: &scheduler.ScheduleTargetEcsParametersNetworkConfigurationArgs{AssignPublicIp: pulumi.Bool(false), Subnets: pulumi.ToStringArray(c.SubnetIDs), SecurityGroups: pulumi.ToStringArray(c.SecurityGroupIDs)}}}}, scheduleOpts...)
		if e != nil {
			return nil, e
		}
		if i == 0 {
			result.Run = scheduled
		} else {
			result.Check = scheduled
		}
	}
	return result, nil
}
