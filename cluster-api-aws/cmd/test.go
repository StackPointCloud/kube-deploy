package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
	"k8s.io/kube-deploy/cluster-api-aws/cloud/aws"
	"k8s.io/kube-deploy/cluster-api-aws/util"
)

type TestOptions struct {
	Cluster        string
	Machine        string
	SSHKeyPath     string
	KubeconfigPath string
}

var opts = &TestOptions{}

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "try something",
	Long:  `try something`,
	Run: func(cmd *cobra.Command, args []string) {
		if opts.Cluster == "" {
			glog.Error("Please provide yaml file for cluster definition.")
			cmd.Help()
			os.Exit(1)
		}
		if opts.Machine == "" {
			glog.Error("Please provide yaml file for machine definition.")
			cmd.Help()
			os.Exit(1)
		}
		if opts.SSHKeyPath == "" {
			glog.Error("Please provide a path containing public and private ssh keys")
			cmd.Help()
			os.Exit(1)
		}
		if err := actuate(opts); err != nil {
			glog.Exit(err)
		}
	},
}

func writeConfigToDisk(config, configPath string) error {
	file, err := os.Create(configPath)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(config); err != nil {
		return err
	}
	defer file.Close()

	file.Sync() // flush
	return nil
}

func actuate(opts *TestOptions) error {
	a, err := aws.NewMachineActuator(util.RandomToken(), opts.SSHKeyPath, nil)
	if err != nil {
		return err
	}
	cluster, err := parseClusterYaml(opts.Cluster)
	if err != nil {
		return err
	}
	machines, err := parseMachinesYaml(opts.Machine)
	if err != nil {
		return err
	}

	err = a.Create(cluster, machines[0])
	if err != nil {
		return err
	}

	var config string
	for i := 0; i <= 40; i++ {

		var err error
		if config, err = a.GetKubeConfig(machines[0]); err != nil || config == "" {
			fmt.Println("Waiting for Kubernetes to come up...")
			time.Sleep(15 * time.Second)
			continue
		}
	}
	writeConfigToDisk(config, opts.KubeconfigPath)
	fmt.Printf("Try:\tkubectl --kubeconfig %s cluster-info\n", opts.KubeconfigPath)
	return nil
}

func init() {
	testCmd.Flags().StringVarP(&opts.Cluster, "cluster", "c", "", "cluster yaml file")
	testCmd.Flags().StringVarP(&opts.Machine, "machines", "m", "", "machine yaml file")
	testCmd.Flags().StringVarP(&opts.SSHKeyPath, "sshkey", "s", "", "ssh key directory")
	testCmd.Flags().StringVarP(&opts.KubeconfigPath, "kubeconfig", "k", "./kubeconfig", "path to kubeconfig")

	RootCmd.AddCommand(testCmd)
}
