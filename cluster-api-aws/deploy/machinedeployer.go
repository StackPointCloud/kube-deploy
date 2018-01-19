package deploy

import (
	"fmt"

	"k8s.io/kube-deploy/cluster-api-aws/cloud"
	"k8s.io/kube-deploy/cluster-api-aws/cloud/aws"
	clusterv1 "k8s.io/kube-deploy/cluster-api/api/cluster/v1alpha1"
)

// Provider-specific machine logic the deployer needs.
type machineDeployer interface {
	cloud.MachineActuator
	GetIP(machine *clusterv1.Machine) (string, error)
	GetKubeConfig(master *clusterv1.Machine) (string, error)

	// Create and start the machine controller. The list of initial
	// machines don't have to be reconciled as part of this function, but
	// are provided in case the function wants to refer to them (and their
	// ProviderConfigs) to know how to configure the machine controller.
	// Not idempotent.
	CreateMachineController(cluster *clusterv1.Cluster, initialMachines []*clusterv1.Machine) error
	PostDelete(cluster *clusterv1.Cluster, machines []*clusterv1.Machine) error
}

func newMachineDeployer(cloud, sshKeyPath, kubeadmToken string) (machineDeployer, error) {
	switch cloud {
	case "aws":
		return aws.NewMachineActuator(sshKeyPath, kubeadmToken, nil)
	default:
		return nil, fmt.Errorf("Not recognized cloud provider: %s\n", cloud)
	}
}
