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

	awsv1alpha1 "k8s.io/kube-deploy/cluster-api-aws/cloud/aws/awsproviderconfig/v1alpha1"
	"k8s.io/kube-deploy/cluster-api-aws/util"

	awssdk "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"

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
	privateKeyPath string
}

type AWSClient struct {
	awsCredentials *credentials.Credentials
	session        *Session
	kubeadmToken   string
	sshCreds       SshCreds
	machineClient  client.MachinesInterface
}

// CreateVolume creates a volume wwith hard-coded parameters, reuurn the volumeID
// https://github.com/aws/aws-sdk-go/blob/master/service/ec2/api.go#L21779-L21838
func (sess *Session) CreateVolume(volumeType string, sizeGB int64) (string, error) {
	var spec ec2.CreateVolumeInput
	spec.SetAvailabilityZone(sess.Zone)
	spec.SetVolumeType(volumeType)
	spec.SetSize(sizeGB)
	spec.SetEncrypted(false)

	svc := ec2.New(sess.Session)
	volume, err := svc.CreateVolume(&spec)
	if err != nil {
		return "", err
	}
	return *volume.VolumeId, nil
}

func NewMachineActuator(kubeadmToken string, machineClient client.MachinesInterface) (*AWSClient, error) {

	// config := &awssdk.Config{
	// 	Region:      awssdk.String(region),
	// 	Credentials: credentials.NewEnvCredentials(),
	// }

	// _, err := config.Credentials.Get()
	// if err != nil {
	// 	panic(err)
	// }

	// sdkSession, err := session.NewSession(config)

	return &AWSClient{
		awsCredentials: credentials.NewEnvCredentials(),
		kubeadmToken:   util.RandomToken(),
	}, nil

}

func getClusterProviderConfig(cluster *clusterv1.Cluster) (*awsv1alpha1.AWSClusterProviderConfig, error) {

	var config awsv1alpha1.AWSClusterProviderConfig

	fmt.Printf("%s\n", cluster.Spec.ProviderConfig)

	if err := json.Unmarshal([]byte(cluster.Spec.ProviderConfig), &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func getMachineProviderConfig(machine *clusterv1.Machine) (*awsv1alpha1.AWSMachineProviderConfig, error) {
	var config awsv1alpha1.AWSMachineProviderConfig
	fmt.Printf("%s\n", machine.Spec.ProviderConfig)

	if err := json.Unmarshal([]byte(machine.Spec.ProviderConfig), &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func (aws *AWSClient) Create(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {

	// is cluster configured alright?  If not configure it
	clusterConfig, err := getClusterProviderConfig(cluster)
	if err != nil {
		return err
	}
	if clusterConfig.Region == "" {
		return fmt.Errorf("Region not specified in cluster configuration")
	}
	fmt.Printf("%s\n", clusterConfig.Region)
	config := &awssdk.Config{
		Region:      awssdk.String(clusterConfig.Region),
		Credentials: aws.awsCredentials,
	}

	sdkSession, err := session.NewSession(config)
	svc := ec2.New(sdkSession)

	// does machine already exist
	machineConfig, err := getMachineProviderConfig(machine)
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", machineConfig.Region)

	//	targetVpcName := "cluster-api-aws"
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
				Values: []*string{awssdk.String("cluster-api-aws")},
			},
		},
	})
	if err != nil {
		return err
	}
	if len(groups.SecurityGroups) == 0 {
		return fmt.Errorf("unable to look up security groups")
	}

	networkSpec := &ec2.InstanceNetworkInterfaceSpecification{

		DeviceIndex: awssdk.Int64(0),

		// Indicates whether to assign a public IPv4 address to an instance you launch
		// in a VPC. The public IP address can only be assigned to a network interface
		// for eth0, and can only be assigned to a new network interface, not an existing
		// one. You cannot specify more than one network interface in the request. If
		// launching into a default subnet, the default value is true.
		AssociatePublicIpAddress: awssdk.Bool(true),

		// If set to true, the interface is deleted when the instance is terminated.
		// You can specify true only if creating a new network interface when launching
		// an instance.
		DeleteOnTermination: awssdk.Bool(true),

		// The IDs of the security groups for the network interface. Applies only if
		// creating a network interface when launching an instance.
		Groups: []*string{awssdk.String("sg-59bda825")},

		// The ID of the subnet associated with the network string. Applies only if
		// creating a network interface when launching an instance.
		SubnetId: subnet.SubnetId,
	}

	// curl https://coreos.com/dist/aws/aws-stable.json | jq .
	// --> for example,
	// },
	// "us-west-1": {
	// "hvm": "ami-e0696980",
	// "pv": "ami-6e68680e"
	// },
	// "us-west-2": {
	// "hvm": "ami-dc4ce6a4",
	// "pv": "ami-3746ec4f"
	// }

	// or consult https://cloud-images.ubuntu.com/locator/ec2/

	userData, err := GetCloudConfig(aws.kubeadmToken, cluster, machine)
	if err != nil {
		return err
	}
	b64UserData := base64.StdEncoding.EncodeToString([]byte(userData))

	// kp := &ec2.ImportKeyPairInput{
	// 	KeyName:           awssdk.String("key-3df566133f69"),
	// 	PublicKeyMaterial: []byte("ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDHyz2moQ66ycWjfShpbk3y1GpaAQR2NB3UuauR1O4UKZBdxsrFzrirli6d/q0krt3A/+xCYiLszCiyLcGRzjuwMkY2a7A1kzkh1bprfwAvSY1lyhyqNEA4UmhOczz46+j7shkI8NOn7+b7eYieA0veNSEsDAJwaQBJZ4u8H1012m5O6/KwiA5Flo6D3NbGQmfuKwpV6t5iAzV1so8AaDtsl2rIVqbKEDFRJxOLI0VLpNPc0fs+acRJP928u/gDghW+TeOpKsvPFGQe/WuwKBseLlRKEXPX4wzQAh99iQMj/PiTrGll+TdS2mqTzV332ABtwxKcA2MeM89x8EO2nM6j nfranzen@esox"),
	// }

	// resp, err := svc.ImportKeyPair(kp)
	// if err != nil {
	// 	return err
	// }
	// fmt.Printf("%s\n", *resp.KeyName)

	tags := []*ec2.TagSpecification{
		&ec2.TagSpecification{
			ResourceType: awssdk.String("instance"),
			Tags: []*ec2.Tag{
				&ec2.Tag{
					Key:   awssdk.String("Name"),
					Value: awssdk.String("cluster-api-aws-instance"),
				},
			},
		},
	}

	runResult, err := svc.RunInstances(&ec2.RunInstancesInput{
		ImageId:      awssdk.String(machineConfig.Image),
		InstanceType: awssdk.String(machineConfig.MachineType),
		// SubnetId:          subnet.SubnetId,
		KeyName:           awssdk.String("key-3df566133f69"),
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

	fmt.Printf("%v\n", runResult.Instances[0].PublicIpAddress)
	return nil

	// return fmt.Errorf("Stopping here")

	// config, err := gce.providerconfig(machine.Spec.ProviderConfig)
	// if err != nil {
	// 	return gce.handleMachineError(machine, apierrors.InvalidMachineConfiguration(
	// 		"Cannot unmarshal providerConfig field: %v", err))
	// }

	// if verr := gce.validateMachine(machine, config); verr != nil {
	// 	return gce.handleMachineError(machine, verr)
	// }

	// var metadata map[string]string
	// if cluster.Spec.ClusterNetwork.DNSDomain == "" {
	// 	return errors.New("invalid cluster configuration: missing Cluster.Spec.ClusterNetwork.DNSDomain")
	// }
	// if getSubnet(cluster.Spec.ClusterNetwork.Pods) == "" {
	// 	return errors.New("invalid cluster configuration: missing Cluster.Spec.ClusterNetwork.Pods")
	// }
	// if getSubnet(cluster.Spec.ClusterNetwork.Services) == "" {
	// 	return errors.New("invalid cluster configuration: missing Cluster.Spec.ClusterNetwork.Services")
	// }
	// if machine.Spec.Versions.Kubelet == "" {
	// 	return errors.New("invalid master configuration: missing Machine.Spec.Versions.Kubelet")
	// }

	// image, preloaded := gce.getImage(machine, config)

	// if apiutil.IsMaster(machine) {
	// 	if machine.Spec.Versions.ControlPlane == "" {
	// 		return gce.handleMachineError(machine, apierrors.InvalidMachineConfiguration(
	// 			"invalid master configuration: missing Machine.Spec.Versions.ControlPlane"))
	// 	}
	// 	var err error
	// 	metadata, err = masterMetadata(
	// 		templateParams{
	// 			Token:     gce.kubeadmToken,
	// 			Cluster:   cluster,
	// 			Machine:   machine,
	// 			Preloaded: preloaded,
	// 		},
	// 	)
	// 	if err != nil {
	// 		return err
	// 	}
	// } else {
	// 	if len(cluster.Status.APIEndpoints) == 0 {
	// 		return errors.New("invalid cluster state: cannot create a Kubernetes node without an API endpoint")
	// 	}
	// 	var err error
	// 	metadata, err = nodeMetadata(
	// 		templateParams{
	// 			Token:     gce.kubeadmToken,
	// 			Cluster:   cluster,
	// 			Machine:   machine,
	// 			Preloaded: preloaded,
	// 		},
	// 	)
	// 	if err != nil {
	// 		return err
	// 	}
	// }

	// var metadataItems []*compute.MetadataItems
	// for k, v := range metadata {
	// 	v := v // rebind scope to avoid loop aliasing below
	// 	metadataItems = append(metadataItems, &compute.MetadataItems{
	// 		Key:   k,
	// 		Value: &v,
	// 	})
	// }

	// instance, err := gce.instanceIfExists(machine)
	// if err != nil {
	// 	return err
	// }

	// name := machine.ObjectMeta.Name
	// project := config.Project
	// zone := config.Zone
	// diskSize := int64(10)

	// // Our preloaded image already has a lot stored on it, so increase the
	// // disk size to have more free working space.
	// if preloaded {
	// 	diskSize = 30
	// }

	// if instance == nil {
	// 	labels := map[string]string{
	// 		UIDLabelKey: fmt.Sprintf("%v", machine.ObjectMeta.UID),
	// 	}
	// 	if gce.machineClient == nil {
	// 		labels[BootstrapLabelKey] = "true"
	// 	}

	// 	op, err := gce.service.Instances.Insert(project, zone, &compute.Instance{
	// 		Name:        name,
	// 		MachineType: fmt.Sprintf("zones/%s/machineTypes/%s", zone, config.MachineType),
	// 		NetworkInterfaces: []*compute.NetworkInterface{
	// 			{
	// 				Network: "global/networks/default",
	// 				AccessConfigs: []*compute.AccessConfig{
	// 					{
	// 						Type: "ONE_TO_ONE_NAT",
	// 						Name: "External NAT",
	// 					},
	// 				},
	// 			},
	// 		},
	// 		Disks: []*compute.AttachedDisk{
	// 			{
	// 				AutoDelete: true,
	// 				Boot:       true,
	// 				InitializeParams: &compute.AttachedDiskInitializeParams{
	// 					SourceImage: image,
	// 					DiskSizeGb:  diskSize,
	// 				},
	// 			},
	// 		},
	// 		Metadata: &compute.Metadata{
	// 			Items: metadataItems,
	// 		},
	// 		Tags: &compute.Tags{
	// 			Items: []string{"https-server"},
	// 		},
	// 		Labels: labels,
	// 	}).Do()

	// 	if err == nil {
	// 		err = gce.waitForOperation(config, op)
	// 	}

	// 	if err != nil {
	// 		return gce.handleMachineError(machine, apierrors.CreateMachine(
	// 			"error creating GCE instance: %v", err))
	// 	}

	// 	// If we have a machineClient, then annotate the machine so that we
	// 	// remember exactly what VM we created for it.
	// 	if gce.machineClient != nil {
	// 		return gce.updateAnnotations(machine)
	// 	}
	// } else {
	// 	glog.Infof("Skipped creating a VM that already exists.\n")
	// }

	return nil
}

// func (gce *AWSClient) handleMachineError(machine *clusterv1.Machine, err *apierrors.MachineError) error {
// 	if gce.machineClient != nil {
// 		reason := err.Reason
// 		message := err.Message
// 		machine.Status.ErrorReason = &reason
// 		machine.Status.ErrorMessage = &message
// 		gce.machineClient.Update(machine)
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
