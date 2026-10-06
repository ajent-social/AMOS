package aws

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	awsec2 "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// NetworkArgs contains owner-selected placement inputs. The availability zone
// and both CIDRs are required so preview cannot silently choose a topology.
type NetworkArgs struct {
	Name             string // stable resource naming prefix
	Environment      string // one of development, staging, production
	InstallationID   string // canonical immutable UUIDv7 installation identity
	AvailabilityZone string // explicit AWS AZ; never selected by discovery
	VPCCIDR          string // explicit private IPv4 VPC CIDR
	PublicSubnetCIDR string // narrower IPv4 CIDR contained by VPCCIDR
}

// ManagementProfileSessionManager selects the only supported host-management policy.
const ManagementProfileSessionManager = "session-manager"

var installationIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var standardRegionPattern = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[0-9]+$`)
var govRegionPattern = regexp.MustCompile(`^[a-z]{2}-gov-[a-z]+-[0-9]+$`)
var chinaRegionPattern = regexp.MustCompile(`^cn-[a-z]+-[0-9]+$`)

func validateOwnerScope(environment, installationID string) error {
	if environment != "development" && environment != "staging" && environment != "production" {
		return fmt.Errorf("environment must be development, staging, or production")
	}
	if !installationIDPattern.MatchString(installationID) {
		return fmt.Errorf("installation ID must be a canonical lowercase UUIDv7")
	}
	return nil
}

func ownerTags(name, environment, installationID string) pulumi.StringMap {
	return pulumi.StringMap{
		"Name":              pulumi.String(name),
		"amos:profile":      pulumi.String("aws_vm"),
		"amos:environment":  pulumi.String(environment),
		"amos:installation": pulumi.String(installationID),
	}
}

func validAWSRegion(region, partition string) bool {
	switch partition {
	case "aws":
		return standardRegionPattern.MatchString(region) && !strings.HasPrefix(region, "cn-") && !strings.Contains(region, "-gov-")
	case "aws-us-gov":
		return govRegionPattern.MatchString(region)
	case "aws-cn":
		return chinaRegionPattern.MatchString(region)
	default:
		return false
	}
}

// Network is the single-AZ public network boundary for the aws_vm profile.
// It intentionally creates no compute, NAT, database, or ingress for SSH/DB.
type Network struct {
	pulumi.ResourceState
	VPCID            pulumi.StringOutput `pulumi:"vpcId"`
	PublicSubnetID   pulumi.StringOutput `pulumi:"publicSubnetId"`
	SecurityGroupID  pulumi.StringOutput `pulumi:"securityGroupId"`
	ElasticIP        pulumi.StringOutput `pulumi:"elasticIp"`
	ElasticIPAllocID pulumi.StringOutput `pulumi:"elasticIpAllocationId"`
}

// NewNetwork creates the explicit single-AZ public network and origin boundary.
func NewNetwork(ctx *pulumi.Context, name string, args NetworkArgs, opts ...pulumi.ResourceOption) (*Network, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(args.Name) == "" {
		return nil, fmt.Errorf("network name is required")
	}
	if strings.TrimSpace(args.AvailabilityZone) == "" {
		return nil, fmt.Errorf("availability zone is required")
	}
	if err := validateOwnerScope(args.Environment, args.InstallationID); err != nil {
		return nil, err
	}
	if err := validateNetworkCIDRs(args.VPCCIDR, args.PublicSubnetCIDR); err != nil {
		return nil, err
	}

	component := &Network{}
	if err := ctx.RegisterComponentResource("amos:infra:aws:Network", name, component, opts...); err != nil {
		return nil, fmt.Errorf("register network component: %w", err)
	}
	childOpts := []pulumi.ResourceOption{pulumi.Parent(component)}
	tags := ownerTags(args.Name, args.Environment, args.InstallationID)

	vpc, err := awsec2.NewVpc(ctx, name+"-vpc", &awsec2.VpcArgs{
		CidrBlock:          pulumi.StringPtr(args.VPCCIDR),
		EnableDnsHostnames: pulumi.BoolPtr(true),
		EnableDnsSupport:   pulumi.BoolPtr(true),
		Tags:               tags,
	}, childOpts...)
	if err != nil {
		return nil, fmt.Errorf("create VPC: %w", err)
	}

	igw, err := awsec2.NewInternetGateway(ctx, name+"-igw", &awsec2.InternetGatewayArgs{
		VpcId: vpc.ID().ToStringPtrOutput(),
		Tags:  ownerTags(args.Name+"-igw", args.Environment, args.InstallationID),
	}, childOpts...)
	if err != nil {
		return nil, fmt.Errorf("create internet gateway: %w", err)
	}

	subnet, err := awsec2.NewSubnet(ctx, name+"-public", &awsec2.SubnetArgs{
		VpcId:               vpc.ID(),
		CidrBlock:           pulumi.StringPtr(args.PublicSubnetCIDR),
		AvailabilityZone:    pulumi.StringPtr(args.AvailabilityZone),
		MapPublicIpOnLaunch: pulumi.BoolPtr(false),
		Tags:                ownerTags(args.Name+"-public", args.Environment, args.InstallationID),
	}, childOpts...)
	if err != nil {
		return nil, fmt.Errorf("create public subnet: %w", err)
	}

	routeTable, err := awsec2.NewRouteTable(ctx, name+"-public-routes", &awsec2.RouteTableArgs{
		VpcId: vpc.ID().ToStringPtrOutput(),
		Routes: awsec2.RouteTableRouteArray{&awsec2.RouteTableRouteArgs{
			CidrBlock: pulumi.StringPtr("0.0.0.0/0"), GatewayId: igw.ID().ToStringPtrOutput(),
		}},
		Tags: ownerTags(args.Name+"-public-routes", args.Environment, args.InstallationID),
	}, childOpts...)
	if err != nil {
		return nil, fmt.Errorf("create public route table: %w", err)
	}
	if _, err = awsec2.NewRouteTableAssociation(ctx, name+"-public-association", &awsec2.RouteTableAssociationArgs{
		RouteTableId: routeTable.ID(), SubnetId: subnet.ID().ToStringPtrOutput(),
	}, childOpts...); err != nil {
		return nil, fmt.Errorf("associate public subnet route table: %w", err)
	}

	ingressRules := originIngressRules()
	if err := validateOriginIngress(ingressRules); err != nil {
		return nil, err
	}
	group, err := awsec2.NewSecurityGroup(ctx, name+"-origin", &awsec2.SecurityGroupArgs{
		VpcId:       vpc.ID().ToStringPtrOutput(),
		Description: pulumi.StringPtr("Direct HTTPS origin ingress for the aws_vm profile"),
		Ingress:     pulumiIngress(ingressRules),
		Egress: awsec2.SecurityGroupEgressArray{&awsec2.SecurityGroupEgressArgs{
			Protocol: pulumi.String("tcp"), FromPort: pulumi.Int(443), ToPort: pulumi.Int(443), CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")}, Description: pulumi.StringPtr("Host package, image and service HTTPS egress")}},
		Tags: ownerTags(args.Name+"-origin", args.Environment, args.InstallationID),
	}, childOpts...)
	if err != nil {
		return nil, fmt.Errorf("create origin security group: %w", err)
	}

	eip, err := awsec2.NewEip(ctx, name+"-origin-ip", &awsec2.EipArgs{
		Domain: pulumi.StringPtr("vpc"), Tags: ownerTags(args.Name+"-origin", args.Environment, args.InstallationID),
	}, childOpts...)
	if err != nil {
		return nil, fmt.Errorf("allocate origin Elastic IP: %w", err)
	}

	component.VPCID = vpc.ID().ToStringOutput()
	component.PublicSubnetID = subnet.ID().ToStringOutput()
	component.SecurityGroupID = group.ID().ToStringOutput()
	component.ElasticIP = eip.PublicIp
	component.ElasticIPAllocID = eip.ID().ToStringOutput()
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"vpcId": component.VPCID, "publicSubnetId": component.PublicSubnetID,
		"securityGroupId": component.SecurityGroupID, "elasticIp": component.ElasticIP,
		"elasticIpAllocationId": component.ElasticIPAllocID,
	}); err != nil {
		return nil, fmt.Errorf("register network outputs: %w", err)
	}
	return component, nil
}

func validateNetworkCIDRs(vpcText, subnetText string) error {
	vpcIP, vpc, err := net.ParseCIDR(vpcText)
	if err != nil || vpcIP.To4() == nil || !vpcIP.IsPrivate() || vpcIP.String() != vpc.IP.String() {
		return fmt.Errorf("VPC CIDR must be a canonical network address")
	}
	subnetIP, subnet, err := net.ParseCIDR(subnetText)
	if err != nil || subnetIP.To4() == nil || subnetIP.String() != subnet.IP.String() || !vpc.Contains(subnet.IP) {
		return fmt.Errorf("public subnet CIDR must be contained by VPC CIDR")
	}
	vpcBits, _ := vpc.Mask.Size()
	subnetBits, _ := subnet.Mask.Size()
	if subnetBits <= vpcBits {
		return fmt.Errorf("public subnet CIDR must be narrower than VPC CIDR")
	}
	if vpcBits < 16 || vpcBits > 28 || subnetBits < 16 || subnetBits > 28 {
		return fmt.Errorf("VPC and subnet CIDRs must be between /16 and /28")
	}
	return nil
}

type ingressRule struct {
	protocol string
	from, to int
	cidrs    []string
}

func originIngressRules() []ingressRule {
	return []ingressRule{
		{protocol: "tcp", from: 80, to: 80, cidrs: []string{"0.0.0.0/0"}},
		{protocol: "tcp", from: 443, to: 443, cidrs: []string{"0.0.0.0/0"}},
	}
}

func validateOriginIngress(rules []ingressRule) error {
	seen := map[int]bool{}
	for _, rule := range rules {
		if rule.protocol != "tcp" || rule.from != rule.to || (rule.from != 80 && rule.from != 443) || len(rule.cidrs) != 1 || rule.cidrs[0] != "0.0.0.0/0" {
			return fmt.Errorf("origin ingress permits only public TCP ports 80 and 443")
		}
		if seen[rule.from] {
			return fmt.Errorf("duplicate origin ingress port %d", rule.from)
		}
		seen[rule.from] = true
	}
	if len(seen) != 2 {
		return fmt.Errorf("origin ingress must include TCP ports 80 and 443")
	}
	return nil
}

func pulumiIngress(rules []ingressRule) awsec2.SecurityGroupIngressArray {
	result := make(awsec2.SecurityGroupIngressArray, 0, len(rules))
	for _, rule := range rules {
		cidrs := make(pulumi.StringArray, 0, len(rule.cidrs))
		for _, cidr := range rule.cidrs {
			cidrs = append(cidrs, pulumi.String(cidr))
		}
		description := "HTTPS origin"
		if rule.from == 80 {
			description = "HTTP certificate challenge and redirect"
		}
		result = append(result, &awsec2.SecurityGroupIngressArgs{Protocol: pulumi.String(rule.protocol), FromPort: pulumi.Int(rule.from), ToPort: pulumi.Int(rule.to), CidrBlocks: cidrs, Description: pulumi.StringPtr(description)})
	}
	return result
}
