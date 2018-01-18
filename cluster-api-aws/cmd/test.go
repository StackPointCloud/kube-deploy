package cmd

import (
	"os"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
	"k8s.io/kube-deploy/cluster-api-aws/cloud/aws"
)

type TestOptions struct {
	Cluster string
	Machine string
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
		if err := actuate(opts); err != nil {
			glog.Exit(err)
		}
	},
}

func actuate(opts *TestOptions) error {
	a, err := aws.NewMachineActuator("", nil)
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
	return nil
}

func init() {
	testCmd.Flags().StringVarP(&opts.Cluster, "cluster", "c", "", "cluster yaml file")
	testCmd.Flags().StringVarP(&opts.Machine, "machines", "m", "", "machine yaml file")

	RootCmd.AddCommand(testCmd)
}
