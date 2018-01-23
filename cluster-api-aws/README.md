# Cluster API AWS Prototype

The Cluster API AWS prototype implements the cluster-api


## Building

```bash
$ cd $GOPATH/src/k8s.io/
$ git clone git@github.com:kubernetes/kube-deploy.git
$ cd kube-deploy/cluster-api-aws
$ dep ensure
$ go build
```

## Creating a cluster

Set the AWS credentials as environment variables, 

```
$ export AWS_ACCESS_KEY=AKIA...QQ
$ export AWS_SECRET_KEY=O16A...rUWI
```

There are three yaml files in this directory describing the cluster.

- cluster.yaml cloud infrastructure parameters
- machines.yaml master instance configuration
- node.yaml worker instance configuration

The AWS cloud infrastructure parameters specify the VPC and security group.  These are not
created in place but must exist beforehand.  For the master machine, the AMI instance is
a ubuntu (xenial) image appropriate for the AWS region.

To create the initial master node, 
```
$ ./cluster-api-aws create -c cluster.yaml  -m machines.yaml -k ./kubeconfig -s $HOME/.ssh
```

A master node has been created using a basic kubeadm script.  The step to install a controller
into the cluster has been skipped, for now, so any controller has to be run outside the cluster.

The custom resource definitions of `cluster` and `machine` have been installed, and can be reviewed with

```
$ kubectl --kubeconfig=./kubeconfig get clusters.cluster.k8s.io -o yaml
$ kubectl --kubeconfig=./kubeconfig get machines.cluster.k8s.io -o yaml
```

To start the controller, we need the kubeadm token. We'll need to take a look at the log file
from the master node, by sshing to that node.  Use the the ssh key to access the startup log
and find the kubeadm token.

```
$ ssh -i $HOME/.ssh/id_rsa ubuntu@54.149.48.227 grep kubeadm /var/log/startup.log
```

Using the token and the kubeconfig file, start a machinecontroller process locally to manage the 
machine specifications.  This process should run in a new shell
```
$ cd machinecontroller
$ go build
$ ./machine-controller --cloud aws --kubeconfig ../kubeconfig  --token 33h1lc.2yi1tfjgufrf9bz3
```

Now that a controller exists (running in a different shell), you can add nodes
```
$ kubectl  --kubeconfig=./kubeconfig create -f  ./nodes.yaml
```

The node addition can be observed, 
```
$ kubectl --kubeconfig=./kubeconfig get node
```

### Upgrading your cluster

not yet implemented 

### Node repair

not yet implementd

## Deleting a node

Deleting the machine CRD will trigger deletion of the kubernetes node object and 
call the aws cloud-provider api to delete the VM.

Find the node name with set=node and delete it
```
$ kubectl --kubeconfig=kubeconfig get machine -l set=node -o name
# kubectl --kubeconfig=kubeconfig delete machines/cluster-api-node-7jlrh
```

## Deleting the cluster

TBD remove the master node from the AWS console