package backupschedule

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	aws "github.com/pulumi/pulumi-aws/sdk/v7/go/aws"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func fixture() ManagedAssets {
	base := "arn:aws:ecs:us-east-1:123456789012:"
	role := func(s string) string { return "arn:aws:iam::123456789012:role/" + s }
	task := func(s string) TaskAssets {
		return TaskAssets{TaskDefinitionARN: base + "task-definition/" + s + ":7", ContainerName: s, ExpectedImage: "registry.example/backup@sha256:" + strings.Repeat("a", 64), Executable: "/opt/amos", ConfigFile: "/etc/amos/backup.json", ExpectedConfigSHA256: strings.Repeat("b", 64), TaskRoleARN: role(s + "-task"), ExecutionRoleARN: role(s + "-execution"), EnvironmentFileARN: "arn:aws:s3:::example-backup-env/" + s + ".env"}
	}
	return ManagedAssets{ClusterARN: base + "cluster/example", ScheduleGroupARN: "arn:aws:scheduler:us-east-1:123456789012:schedule-group/backup", RunScheduleName: "backup-run", CheckScheduleName: "backup-check", RunSchedulerRoleARN: role("backup-run-invoke"), CheckSchedulerRoleARN: role("backup-check-invoke"), SubnetIDs: []string{"subnet-01234567"}, SecurityGroupIDs: []string{"sg-01234567"}, Run: task("run"), Check: task("check")}
}
func TestDeclarationRejectsAuthorityAndMutableTargets(t *testing.T) {
	cases := map[string]func(*ManagedAssets){
		"mutable": func(c *ManagedAssets) { c.Run.TaskDefinitionARN = strings.TrimSuffix(c.Run.TaskDefinitionARN, ":7") },
		"zero_revision": func(c *ManagedAssets) {
			c.Run.TaskDefinitionARN = strings.TrimSuffix(c.Run.TaskDefinitionARN, "7") + "0"
		},
		"cross_account": func(c *ManagedAssets) {
			c.Run.TaskRoleARN = strings.Replace(c.Run.TaskRoleARN, "123456789012", "123456789013", 1)
		},
		"shared_task_role":         func(c *ManagedAssets) { c.Check.TaskRoleARN = c.Run.TaskRoleARN },
		"shared_secret_file":       func(c *ManagedAssets) { c.Check.EnvironmentFileARN = c.Run.EnvironmentFileARN },
		"mutable_image":            func(c *ManagedAssets) { c.Run.ExpectedImage = "registry.example/backup:latest" },
		"public_network_injection": func(c *ManagedAssets) { c.SubnetIDs = []string{"subnet-01234567\nAssignPublicIp=true"} },
		"duplicate_network":        func(c *ManagedAssets) { c.SubnetIDs = append(c.SubnetIDs, c.SubnetIDs[0]) },
		"path_expansion":           func(c *ManagedAssets) { c.Run.ConfigFile = "/etc/%h/backup" },
		"role_alias":               func(c *ManagedAssets) { c.RunSchedulerRoleARN = c.Run.TaskRoleARN },
	}
	for n, change := range cases {
		t.Run(n, func(t *testing.T) {
			c := fixture()
			change(&c)
			if _, e := Describe(c); e == nil {
				t.Fatal("invalid authority accepted")
			}
		})
	}
	d, e := Describe(fixture())
	if e != nil {
		t.Fatal(e)
	}
	for _, policy := range []string{d.RunPolicy, d.CheckPolicy, d.RunTrust, d.CheckTrust} {
		if strings.Contains(policy, "\"Resource\":\"*\"") || strings.Contains(policy, "schedule/*") {
			t.Fatal("wildcard authority")
		}
	}
	var trust map[string]any
	if json.Unmarshal([]byte(d.RunTrust), &trust) != nil {
		t.Fatal("invalid trust JSON")
	}
	if !strings.Contains(d.RunTrust, fixture().ScheduleGroupARN) || strings.Contains(d.RunTrust, ":schedule/") {
		t.Fatal("trust must use exact group source ARN")
	}
	s, e := Override(fixture().Run, "run")
	if e != nil {
		t.Fatal(e)
	}
	var got overrides
	if json.Unmarshal([]byte(s), &got) != nil {
		t.Fatal("invalid override")
	}
	v := got.ContainerOverrides[0]
	if strings.Join(v.Command, " ") != "backup run --config /etc/amos/backup.json" || len(v.EnvironmentFiles) != 1 || v.EnvironmentFiles[0].Type != "s3" || v.EnvironmentFiles[0].Value != fixture().Run.EnvironmentFileARN {
		t.Fatal("incorrect invocation")
	}
}

type mocks struct {
	mu        sync.Mutex
	resources []pulumi.MockResourceArgs
	account   string
}

func (m *mocks) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) {
	switch a.Token {
	case "aws:index/getCallerIdentity:getCallerIdentity":
		return resource.NewPropertyMapFromMap(map[string]any{"accountId": m.account, "arn": "arn:aws:iam::" + m.account + ":user/example", "userId": "example"}), nil
	case "aws:index/getRegion:getRegion":
		return resource.NewPropertyMapFromMap(map[string]any{"name": "us-east-1", "region": "us-east-1"}), nil
	}
	return a.Args, nil
}
func (m *mocks) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources = append(m.resources, a)
	return a.Name + "-id", a.Inputs, nil
}
func TestComponentActualResourceGraph(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "activated"}[active], func(t *testing.T) {
			m := &mocks{account: "123456789012"}
			e := pulumi.RunErr(func(ctx *pulumi.Context) error {
				p, e := aws.NewProvider(ctx, "selected", &aws.ProviderArgs{Region: pulumi.String("us-east-1")})
				if e != nil {
					return e
				}
				_, e = New(ctx, "backup", fixture(), active, p)
				return e
			}, pulumi.WithMocks("project", "stack", m))
			if e != nil {
				t.Fatal(e)
			}
			count, policies := 0, 0
			for _, r := range m.resources {
				switch r.TypeToken {
				case "aws:scheduler/schedule:Schedule":
					count++
					if r.RegisterRPC == nil {
						t.Fatal("missing resource registration")
					}
					invocation := "backup-run-invoke"
					if r.Name == "backup-check" {
						invocation = "backup-check-invoke"
					}
					found := false
					for _, dep := range r.RegisterRPC.GetDependencies() {
						if strings.HasSuffix(dep, "::"+invocation) {
							found = true
						}
					}
					if !found {
						t.Fatal("schedule can precede invocation policy")
					}
					v := r.Inputs.Mappable()
					expected := "DISABLED"
					if active {
						expected = "ENABLED"
					}
					if v["state"] != expected || v["scheduleExpression"] != "rate(1 minute)" || v["scheduleExpressionTimezone"] != "UTC" {
						t.Fatal("bad cadence/state", v)
					}
					target := v["target"].(map[string]any)
					retry := target["retryPolicy"].(map[string]any)
					ecs := target["ecsParameters"].(map[string]any)
					network := ecs["networkConfiguration"].(map[string]any)
					if retry["maximumRetryAttempts"] != float64(0) || retry["maximumEventAgeInSeconds"] != float64(60) || ecs["taskCount"] != float64(1) || ecs["platformVersion"] != "1.4.0" || ecs["enableExecuteCommand"] != false || network["assignPublicIp"] != false {
						t.Fatal("unsafe invocation", target)
					}
				case "aws:iam/rolePolicy:RolePolicy":
					policies++
				}
			}
			if count != 2 || policies != 2 {
				t.Fatalf("unexpected graph schedules=%d policies=%d", count, policies)
			}
		})
	}
}
func TestComponentRefusesProviderAccountBeforeSchedules(t *testing.T) {
	m := &mocks{account: "123456789013"}
	e := pulumi.RunErr(func(ctx *pulumi.Context) error {
		p, e := aws.NewProvider(ctx, "selected", nil)
		if e != nil {
			return e
		}
		_, e = New(ctx, "backup", fixture(), false, p)
		return e
	}, pulumi.WithMocks("project", "stack", m))
	if e == nil {
		t.Fatal("wrong account accepted")
	}
	for _, r := range m.resources {
		if strings.HasPrefix(r.TypeToken, "aws:scheduler/") || strings.HasPrefix(r.TypeToken, "aws:iam/") {
			t.Fatal("resource before identity guard")
		}
	}
}
