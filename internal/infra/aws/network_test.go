package aws

import (
	"strings"
	"sync"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type recordedResource struct {
	token  string
	name   string
	inputs resource.PropertyMap
}
type awsMocks struct {
	mu        sync.Mutex
	resources []recordedResource
}

func (m *awsMocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return resource.PropertyMap{}, nil
}
func (m *awsMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources = append(m.resources, recordedResource{token: args.TypeToken, name: args.Name, inputs: args.Inputs})
	outputs := args.Inputs.Copy()
	if args.TypeToken == "aws:ec2/eip:Eip" {
		outputs[resource.PropertyKey("publicIp")] = resource.NewStringProperty("198.51.100.12")
	}
	return args.Name + "-id", outputs, nil
}
func (m *awsMocks) all() []recordedResource {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recordedResource(nil), m.resources...)
}

func TestNetworkPulumiMocksCreateBoundedSingleAZOrigin(t *testing.T) {
	mocks := &awsMocks{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		_, err := NewNetwork(ctx, "reference", NetworkArgs{Name: "example", Environment: "staging", InstallationID: "01890f3e-7c00-7000-8000-000000000001", AvailabilityZone: "us-east-1a", VPCCIDR: "10.24.0.0/16", PublicSubnetCIDR: "10.24.1.0/24"})
		return err
	}, pulumi.WithMocks("amos", "test", mocks))
	if err != nil {
		t.Fatal(err)
	}
	resources := mocks.all()
	find := func(token string) (resource.PropertyMap, bool) {
		for _, r := range resources {
			if r.token == token {
				return r.inputs, true
			}
		}
		return nil, false
	}
	vpc, ok := find("aws:ec2/vpc:Vpc")
	if !ok {
		t.Fatal("VPC missing")
	}
	if got := vpc["cidrBlock"].StringValue(); got != "10.24.0.0/16" {
		t.Fatalf("VPC CIDR = %q", got)
	}
	assertOwnerTags(t, vpc["tags"].ObjectValue(), "staging", "01890f3e-7c00-7000-8000-000000000001")
	subnet, ok := find("aws:ec2/subnet:Subnet")
	if !ok {
		t.Fatal("subnet missing")
	}
	if subnet["availabilityZone"].StringValue() != "us-east-1a" {
		t.Fatal("subnet did not use explicit AZ")
	}
	if subnet["mapPublicIpOnLaunch"].BoolValue() {
		t.Fatal("subnet implicitly assigns public IPs")
	}
	assertOwnerTags(t, subnet["tags"].ObjectValue(), "staging", "01890f3e-7c00-7000-8000-000000000001")
	if _, ok := find("aws:ec2/internetGateway:InternetGateway"); !ok {
		t.Fatal("internet gateway missing")
	}
	route, ok := find("aws:ec2/routeTable:RouteTable")
	if !ok {
		t.Fatal("route table missing")
	}
	if len(route["routes"].ArrayValue()) != 1 || route["routes"].ArrayValue()[0].ObjectValue()["cidrBlock"].StringValue() != "0.0.0.0/0" {
		t.Fatalf("unexpected public route: %v", route["routes"])
	}
	group, ok := find("aws:ec2/securityGroup:SecurityGroup")
	if !ok {
		t.Fatal("security group missing")
	}
	if err := validateMockIngress(group["ingress"].ArrayValue()); err != nil {
		t.Fatal(err)
	}
	egress := group["egress"].ArrayValue()
	if len(egress) != 1 {
		t.Fatalf("got %d egress rules, want one HTTPS rule", len(egress))
	}
	egressRule := egress[0].ObjectValue()
	if egressRule["protocol"].StringValue() != "tcp" || egressRule["fromPort"].NumberValue() != 443 || egressRule["toPort"].NumberValue() != 443 || len(egressRule["cidrBlocks"].ArrayValue()) != 1 || egressRule["cidrBlocks"].ArrayValue()[0].StringValue() != "0.0.0.0/0" {
		t.Fatalf("unexpected host egress rule: %v", egressRule)
	}
	assertOwnerTags(t, group["tags"].ObjectValue(), "staging", "01890f3e-7c00-7000-8000-000000000001")
	eip, ok := find("aws:ec2/eip:Eip")
	if !ok {
		t.Fatal("Elastic IP allocation missing")
	}
	if eip["domain"].StringValue() != "vpc" || eip["instance"].IsString() {
		t.Fatalf("EIP must be allocated but not automatically associated: %v", eip)
	}
	assertOwnerTags(t, eip["tags"].ObjectValue(), "staging", "01890f3e-7c00-7000-8000-000000000001")
	eipCount := 0
	for _, resource := range resources {
		if strings.Contains(resource.token, "natGateway") {
			t.Fatal("single-AZ public profile unexpectedly created a NAT gateway")
		}
		if strings.Contains(resource.token, "loadBalancer") {
			t.Fatal("direct-origin profile unexpectedly created a load balancer")
		}
		if resource.token == "aws:ec2/eip:Eip" {
			eipCount++
		}
	}
	if eipCount != 1 {
		t.Fatalf("allocated %d public IPv4 addresses, want one", eipCount)
	}
}

func TestNetworkRequiresFrozenOwnerScope(t *testing.T) {
	if err := validateOwnerScope("production", "not-a-uuid"); err == nil {
		t.Fatal("accepted noncanonical installation ID")
	}
	if err := validateOwnerScope("sandbox", "01890f3e-7c00-7000-8000-000000000001"); err == nil {
		t.Fatal("accepted unsupported environment")
	}
}

func TestNetworkRejectsPublicPostgresIngressFixture(t *testing.T) {
	fixture := originIngressRules()
	fixture = append(fixture, ingressRule{protocol: "tcp", from: 5432, to: 5432, cidrs: []string{"0.0.0.0/0"}})
	if err := validateOriginIngress(fixture); err == nil {
		t.Fatal("public PostgreSQL fixture was accepted")
	}
}

func TestNetworkRejectsInvalidExplicitCIDRs(t *testing.T) {
	cases := []struct{ name, vpc, subnet string }{
		{"outside VPC", "10.24.0.0/16", "10.25.1.0/24"},
		{"same-sized network", "10.24.0.0/16", "10.24.0.0/16"},
		{"non-canonical network", "10.24.0.1/16", "10.24.1.0/24"},
		{"invalid range", "10.24.0.0/15", "10.24.1.0/24"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateNetworkCIDRs(tc.vpc, tc.subnet); err == nil {
				t.Fatalf("accepted invalid CIDRs %s / %s", tc.vpc, tc.subnet)
			}
		})
	}
}

func TestNetworkAcceptsCanonicalPrivateVPCAndContainedSubnet(t *testing.T) {
	if err := validateNetworkCIDRs("10.24.0.0/16", "10.24.1.0/24"); err != nil {
		t.Fatalf("rejected valid VPC/subnet CIDRs: %v", err)
	}
}

func validateMockIngress(rules []resource.PropertyValue) error {
	converted := make([]ingressRule, 0, len(rules))
	for _, value := range rules {
		props := value.ObjectValue()
		cidrs := make([]string, 0)
		for _, c := range props["cidrBlocks"].ArrayValue() {
			cidrs = append(cidrs, c.StringValue())
		}
		converted = append(converted, ingressRule{protocol: props["protocol"].StringValue(), from: int(props["fromPort"].NumberValue()), to: int(props["toPort"].NumberValue()), cidrs: cidrs})
	}
	return validateOriginIngress(converted)
}

func assertOwnerTags(t *testing.T, tags resource.PropertyMap, environment, installation string) {
	t.Helper()
	if tags["amos:profile"].StringValue() != "aws_vm" || tags["amos:environment"].StringValue() != environment || tags["amos:installation"].StringValue() != installation {
		t.Fatalf("missing immutable owner-scope tags: %v", tags)
	}
}
