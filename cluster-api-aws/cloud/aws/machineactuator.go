/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aws

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	awsv1alpha1 "k8s.io/kube-deploy/cluster-api-aws/cloud/aws/awsproviderconfig/v1alpha1"
	"k8s.io/kube-deploy/cluster-api-aws/util"

	awssdk "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/glog"

	clusterv1 "k8s.io/kube-deploy/cluster-api/api/cluster/v1alpha1"
	"k8s.io/kube-deploy/cluster-api/client"
)

const (
	// Region a default setting
	Region = "us-west-2"
	// Zone a default setting
	Zone = "us-west-2a"
)

type Session struct {
	Region  string
	Zone    string
	Session *session.Session
}

// GetSession creates a session from environment variables
func GetSession(region, zone string) (*Session, error) {
	config := &awssdk.Config{
		Region:      awssdk.String(region),
		Credentials: credentials.NewEnvCredentials(),
	}

	_, err := config.Credentials.Get()
	if err != nil {
		panic(err)
	}

	sdkSession, err := session.NewSession(config)

	return &Session{
		Region:  region,
		Zone:    zone,
		Session: sdkSession,
	}, err

}

type SshCreds struct {
	user           string
	publicKeyPath  string
	privateKeyPath string
}

type AWSClient struct {
	awsCredentials *credentials.Credentials
	session        *Session
	kubeadmToken   string
	sshCreds       SshCreds
	machineClient  client.MachinesInterface
}

func NewMachineActuator(sshKeyPath, kubeadmToken string, machineClient client.MachinesInterface) (*AWSClient, error) {

	sshCreds := SshCreds{
		user:           "ubuntu",
		privateKeyPath: path.Join(sshKeyPath, "id_rsa"),
		publicKeyPath:  path.Join(sshKeyPath, "id_rsa.pub"),
	}
	if _, err := os.Stat(sshCreds.publicKeyPath); err != nil {
		return nil, fmt.Errorf("Problem acesssing file path %s", sshCreds.publicKeyPath)
	}
	if _, err := os.Stat(sshCreds.privateKeyPath); err != nil {
		return nil, fmt.Errorf("Problem acesssing file path %s", sshCreds.privateKeyPath)
	}

	return &AWSClient{
		awsCredentials: credentials.NewEnvCredentials(),
		kubeadmToken:   kubeadmToken,
		sshCreds:       sshCreds,
	}, nil

}

func getClusterProviderConfig(cluster *clusterv1.Cluster) (*awsv1alpha1.AWSClusterProviderConfig, error) {
	var config awsv1alpha1.AWSClusterProviderConfig
	// fmt.Printf("%s\n", cluster.Spec.ProviderConfig)
	if err := json.Unmarshal([]byte(cluster.Spec.ProviderConfig), &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func getMachineProviderConfig(machine *clusterv1.Machine) (*awsv1alpha1.AWSMachineProviderConfig, error) {
	var config awsv1alpha1.AWSMachineProviderConfig
	// fmt.Printf("%s\n", machine.Spec.ProviderConfig)
	if err := json.Unmarshal([]byte(machine.Spec.ProviderConfig), &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// Delete the machine.
func (aws *AWSClient) Delete(*clusterv1.Machine) error {
	return fmt.Errorf("Delete - NotYetImplemented")
}

// Update the machine to the provided definition.
func (aws *AWSClient) Update(c *clusterv1.Cluster, machine *clusterv1.Machine) error {
	return fmt.Errorf("Update - NotYetImplemented")
}

// Checks if the machine currently exists.
func (aws *AWSClient) Exists(machine *clusterv1.Machine) (bool, error) {
	svc, err := aws.getAwsService(nil, machine)
	if err != nil {
		return false, err
	}
	// lookup by name
	instanceRequest := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name:   awssdk.String("tag:Name"),
				Values: []*string{awssdk.String(machine.ObjectMeta.Name)},
			},
		},
	}
	instances, err := svc.DescribeInstances(instanceRequest)
	if len(instances.Reservations) == 0 {
		return false, nil
	}
	if len(instances.Reservations) > 1 {
		return false, fmt.Errorf("Found multiple instance reservation for name %s", machine.ObjectMeta.Name)
	}
	if len(instances.Reservations[0].Instances) == 0 {
		return false, nil
	}
	if len(instances.Reservations[0].Instances) > 1 {
		return false, fmt.Errorf("Found multiple instances for name %s", machine.ObjectMeta.Name)
	}
	return true, nil
}

func (aws *AWSClient) CreateMachineController(cluster *clusterv1.Cluster, initialMachines []*clusterv1.Machine) error {
	return fmt.Errorf("CreateMachineController - NotYetImplemented")
}

func (aws *AWSClient) PostDelete(cluster *clusterv1.Cluster, machines []*clusterv1.Machine) error {
	return fmt.Errorf("PostDelete - NotYetImplemented")
}

// GetIP looks up the machine by name (tag) and finds the public ip
func (aws *AWSClient) GetIP(machine *clusterv1.Machine) (string, error) {
	svc, err := aws.getAwsService(nil, machine)
	if err != nil {
		return "", err
	}

	// lookup by name ... getting the public ip.  This *is* the canonical way to get an instance by the name tag
	instanceRequest := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name:   awssdk.String("tag:Name"),
				Values: []*string{awssdk.String(machine.ObjectMeta.Name)},
			},
		},
	}
	instances, err := svc.DescribeInstances(instanceRequest)
	if len(instances.Reservations) != 1 {
		return "", fmt.Errorf("Failed to lookup single instance reservation for name %s", machine.ObjectMeta.Name)
	}
	if len(instances.Reservations[0].Instances) != 1 {
		return "", fmt.Errorf("Failed to lookup single instance for name %s", machine.ObjectMeta.Name)
	}
	ip := instances.Reservations[0].Instances[0].PublicIpAddress
	if ip == nil {
		return "", fmt.Errorf("Public IP address is nil for instance name %s", machine.ObjectMeta.Name)
	}
	return *ip, nil
}

// getAwsService obtains a service for the credentials and region of the cluster or machine
func (aws *AWSClient) getAwsService(cluster *clusterv1.Cluster, machine *clusterv1.Machine) (*ec2.EC2, error) {
	var region string
	if cluster != nil {
		clusterConfig, err := getClusterProviderConfig(cluster)
		if err != nil {
			return nil, err
		}
		if clusterConfig.Region == "" {
			return nil, fmt.Errorf("Region not specified in cluster configuration")
		}
		region = clusterConfig.Region
	} else {
		if machine == nil {
			return nil, fmt.Errorf("Cannot get sesssion for nil cluster and nil machine")
		}
		machineConfig, err := getMachineProviderConfig(machine)
		if err != nil {
			return nil, err
		}
		if machineConfig.Region == "" {
			return nil, fmt.Errorf("Region not specified in machine configuration")
		}
		region = machineConfig.Region
	}
	config := &awssdk.Config{
		Region:      awssdk.String(region),
		Credentials: aws.awsCredentials,
	}

	sdkSession, err := session.NewSession(config)
	if err != nil {
		return nil, err
	}
	svc := ec2.New(sdkSession)
	return svc, nil
}

func (aws *AWSClient) Create(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {

	svc, err := aws.getAwsService(cluster, machine)
	if err != nil {
		return err
	}

	machineConfig, err := getMachineProviderConfig(machine)
	if err != nil {
		return err
	}
	// fmt.Printf("%s\n", machine.ObjectMeta.Name)
	// fmt.Printf("%s\n", machine.ObjectMeta.GenerateName)

	//	targetVpcName := "cluster-api-aws"
	clusterConfig, _ := getClusterProviderConfig(cluster)
	targetVpcName := clusterConfig.VpcName

	var vpc *ec2.Vpc
	descriptor := &ec2.DescribeVpcsInput{}
	vpcs, err := svc.DescribeVpcs(descriptor)
	if err != nil {
		return err
	}
	for _, v := range vpcs.Vpcs {
		for _, tag := range v.Tags {
			if *tag.Key == "Name" && *tag.Value == targetVpcName {
				vpc = v
				fmt.Printf("%s  %s  %s\n", *tag.Value, *v.CidrBlock, *v.VpcId)
			}
		}

	}
	if vpc == nil {
		return fmt.Errorf("VPC %s not found", targetVpcName)
	}
	if *vpc.CidrBlock != clusterConfig.VpcCIDR {
		return fmt.Errorf("VPC %s cidr (%s) does not match requested cidr (%s)", targetVpcName, *vpc.CidrBlock, clusterConfig.VpcCIDR)
	}

	// return fmt.Errorf("stop")

	var subnet *ec2.Subnet
	subnets, err := svc.DescribeSubnets(&ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name:   awssdk.String("vpc-id"),
				Values: []*string{vpc.VpcId},
			},
			&ec2.Filter{
				Name:   awssdk.String("cidrBlock"),
				Values: []*string{awssdk.String(machineConfig.SubnetCIDR)},
			},
		},
	})
	if err != nil {
		return err
	}
	if len(subnets.Subnets) > 0 {
		subnet = subnets.Subnets[0]
	} else {
		subnetCreation, err := svc.CreateSubnet(&ec2.CreateSubnetInput{
			CidrBlock: awssdk.String(machineConfig.SubnetCIDR),
			VpcId:     vpc.VpcId,
		})
		if err != nil {
			return err
		}
		subnet = subnetCreation.Subnet
	}

	groups, err := svc.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name:   awssdk.String("group-name"),
				Values: []*string{awssdk.String(targetVpcName)},
			},
		},
	})
	if err != nil {
		return err
	}
	if len(groups.SecurityGroups) == 0 {
		return fmt.Errorf("unable to look up security groups for name %s", targetVpcName)
	}
	sg := groups.SecurityGroups[0]

	// sgResponse, err := svc.CreateSecurityGroup(&ec2.CreateSecurityGroupInput{
	// 	GroupName: awssdk.String(clusterConfig.VpcName),
	// 	VpcId:     vpc.VpcId,
	// })

	// sgRules :=  &AuthorizeSecurityGroupIngressInput  {
	// 	CidrIp    *string    `type:"string"`
	// 	FromPort    *int64    `type:"integer"`
	// 	GroupId    *string    `type:"string"`
	// 	GroupName    *string    `type:"string"`
	// 	IpPermissions    []*IpPermission    `locationNameList:"item" type:"list"`
	// 	IpProtocol    *string    `type:"string"`
	// 	SourceSecurityGroupName    *string    `type:"string"`
	// 	SourceSecurityGroupOwnerId    *string    `type:"string"`
	// 	ToPort    *int64    `type:"integer"`
	// }

	networkSpec := &ec2.InstanceNetworkInterfaceSpecification{
		DeviceIndex:              awssdk.Int64(0),
		AssociatePublicIpAddress: awssdk.Bool(true),
		DeleteOnTermination:      awssdk.Bool(true),
		Groups:                   []*string{sg.GroupId},
		SubnetId:                 subnet.SubnetId,
	}

	// ubuntu AMIs - consult https://cloud-images.ubuntu.com/locator/ec2/

	userData, err := GetCloudConfig(aws.kubeadmToken, cluster, machine)
	if err != nil {
		return err
	}
	b64UserData := base64.StdEncoding.EncodeToString([]byte(userData))

	// set up ssh key in AWS if not already present
	sshKeyName := fmt.Sprintf("sshkey-%s", cluster.ObjectMeta.Name)
	keypairs, err := svc.DescribeKeyPairs(&ec2.DescribeKeyPairsInput{
		KeyNames: []*string{awssdk.String(sshKeyName)},
	})
	if err != nil || len(keypairs.KeyPairs) == 0 {
		content, err := ioutil.ReadFile(aws.sshCreds.publicKeyPath)
		if err != nil {
			return err
		}
		kp := &ec2.ImportKeyPairInput{
			KeyName:           awssdk.String(sshKeyName),
			PublicKeyMaterial: content,
		}
		_, err = svc.ImportKeyPair(kp)
		if err != nil {
			return err
		}
	}

	tags := []*ec2.TagSpecification{
		&ec2.TagSpecification{
			ResourceType: awssdk.String("instance"),
			Tags: []*ec2.Tag{
				&ec2.Tag{
					Key:   awssdk.String("Name"),
					Value: awssdk.String(machine.ObjectMeta.Name),
				},
			},
		},
	}

	// return fmt.Errorf("stop")

	runResult, err := svc.RunInstances(&ec2.RunInstancesInput{
		ImageId:           awssdk.String(machineConfig.Image),
		InstanceType:      awssdk.String(machineConfig.MachineType),
		KeyName:           awssdk.String(sshKeyName),
		NetworkInterfaces: []*ec2.InstanceNetworkInterfaceSpecification{networkSpec},
		MinCount:          awssdk.Int64(1),
		MaxCount:          awssdk.Int64(1),
		UserData:          awssdk.String(b64UserData),
		TagSpecifications: tags,
	})

	if err != nil {
		return err
	}

	if len(runResult.Instances) != 1 {
		return fmt.Errorf("seems weird")
	}

	// fmt.Printf("%v\n", runResult.Instances[0].State)
	instanceID := runResult.Instances[0].InstanceId

	statusRequest := &ec2.DescribeInstanceStatusInput{
		InstanceIds:         []*string{instanceID},
		IncludeAllInstances: awssdk.Bool(true),
	}
	err = util.WaitForCondition(
		5*time.Second,
		10*time.Minute,
		"Instance in running state",
		func() (bool, error) {
			status, err := svc.DescribeInstanceStatus(statusRequest)
			if err != nil {
				return false, err
			}
			if len(status.InstanceStatuses) == 0 {
				return false, fmt.Errorf("Instance %s not found", *instanceID)
			}
			fmt.Printf("%s Status: %s\n", *instanceID, *status.InstanceStatuses[0].InstanceState.Name)
			if "running" == *status.InstanceStatuses[0].InstanceState.Name {
				return true, nil
			}
			return false, nil
		})

	if err != nil {
		return err
	}

	ip, err := aws.GetIP(machine)
	if err != nil {
		return err
	}
	fmt.Printf("Ip address is %s\n", ip)
	return nil
}

// func (aws *AWSClient) handleMachineError(machine *clusterv1.Machine, err *apierrors.MachineError) error {
// 	if aws.machineClient != nil {
// 		reason := err.Reason
// 		message := err.Message
// 		machine.Status.ErrorReason = &reason
// 		machine.Status.ErrorMessage = &message
// 		aws.machineClient.Update(machine)
// 	}

// 	glog.Errorf("Machine error: %v", err.Message)
// 	return err
// }

// func (gce *GCEClient) getImage(machine *clusterv1.Machine, config *gceconfig.GCEProviderConfig) (image string, isPreloaded bool) {
// 	project := config.Project
// 	imgName := "prebaked-ubuntu-1604-lts"
// 	fullName := fmt.Sprintf("projects/%s/global/images/%s", project, imgName)

// 	// Check to see if a preloaded image exists in this project. If so, use it.
// 	_, err := gce.service.Images.Get(project, imgName).Do()
// 	if err == nil {
// 		return fullName, true
// 	}

// 	// Otherwise, fall back to the non-preloaded base image.
// 	return "projects/ubuntu-os-cloud/global/images/family/ubuntu-1604-lts", false
// }

// Just a temporary hack to grab a single range from the config.
// func getSubnet(netRange clusterv1.NetworkRanges) string {
// 	if len(netRange.CIDRBlocks) == 0 {
// 		return ""
// 	}
// 	return netRange.CIDRBlocks[0]
// }

func (aws *AWSClient) remoteSshCommand(m *clusterv1.Machine, cmd string) (string, error) {
	glog.Infof("Remote SSH execution '%s' on %s", cmd, m.ObjectMeta.Name)

	publicIP, err := aws.GetIP(m)
	if err != nil {
		return "", err
	}

	command := fmt.Sprintf("echo STARTFILE; %s", cmd)
	c := exec.Command("ssh", "-o", "StrictHostKeyChecking no", "-i", aws.sshCreds.privateKeyPath, aws.sshCreds.user+"@"+publicIP, command)
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error: %v, output: %s", err, string(out))
	}
	result := strings.TrimSpace(string(out))
	parts := strings.Split(result, "STARTFILE")
	if len(parts) != 2 {
		return "", nil
	}
	// TODO: Check error.
	return strings.TrimSpace(parts[1]), nil
}

func (aws *AWSClient) GetKubeConfig(master *clusterv1.Machine) (string, error) {

	command := "sudo cat /etc/kubernetes/admin.conf"
	config, err := aws.remoteSshCommand(master, command)
	if err != nil {
		return "", err
	}
	// remove text before start of config
	re := regexp.MustCompile("apiVersion: ")
	offsets := re.FindStringIndex(config)
	return config[offsets[0]:], nil
}
