/*
Copyright 2020 Critical Stack, LLC
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

package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	cinderapi "github.com/criticalstack/crit/cmd/cinder/api"
	nodeutil "github.com/criticalstack/crit/pkg/kubernetes/util/node"
	machinev1 "github.com/criticalstack/machine-api/api/v1alpha1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/kind/pkg/cluster"

	infrav1 "github.com/criticalstack/machine-api-provider-docker/api/v1alpha1"
)

// NodeReconciler reconciles a corev1.Node object and creates DockerMachine
// objects for nodes where one does not exist. This ensures that even nodes
// that were created outside of the machine-api are described by Kubernetes
// resources.
type NodeReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	config *rest.Config
}

func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	r.config = mgr.GetConfig()
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		WithOptions(options).
		Complete(r)
}

// +kubebuilder:rbac:groups=infrastructure.crit.sh,resources=dockermachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.crit.sh,resources=dockermachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=machine.crit.sh,resources=machines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=machine.crit.sh,resources=machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=machine.crit.sh,resources=configs;configs/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;create;update;patch;delete

func (r *NodeReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("node", req.NamespacedName)

	n := &corev1.Node{}
	if err := r.Get(ctx, req.NamespacedName, n); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// TODO: branch here on node NotReady and check provider api for terminated
	// machines, and delete machine if necessary (since no longer valid

	annotations := n.GetAnnotations()
	if _, ok := annotations[infrav1.NodeOwnerLabelName]; !ok {
		log.Info("dockermachine label not found")
		if err := r.ensureDockerMachineForNode(ctx, n); err != nil {
			return ctrl.Result{}, err
		}
	}
	if refData, ok := annotations[machinev1.NodeOwnerLabelName]; ok {
		var ref corev1.ObjectReference
		if err := json.Unmarshal([]byte(refData), &ref); err != nil {
			return ctrl.Result{}, err
		}
		dockerRefData, ok := annotations[infrav1.NodeOwnerLabelName]
		if !ok {
			return ctrl.Result{}, errors.New("cannot find DockerMachine, missing infra annotation")
		}
		var dmRef corev1.ObjectReference
		if err := json.Unmarshal([]byte(dockerRefData), &dmRef); err != nil {
			return ctrl.Result{}, err
		}
		dm := &infrav1.DockerMachine{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: metav1.NamespaceSystem, Name: dmRef.Name}, dm); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.ensureMachineHasInfraRef(ctx, dm, ref); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// TODO(chrism): move this to the machine controller
func (r *NodeReconciler) ensureMachineHasInfraRef(ctx context.Context, dm *infrav1.DockerMachine, ref corev1.ObjectReference) error {
	m := &machinev1.Machine{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: metav1.NamespaceSystem, Name: ref.Name}, m); err != nil {
		return err
	}
	if m.Spec.InfrastructureRef != nil && m.Spec.InfrastructureRef.Kind == "DockerMachine" && m.Spec.InfrastructureRef.Name == dm.Name {
		return nil
	}
	m.Spec.InfrastructureRef = &corev1.ObjectReference{
		APIVersion: dm.APIVersion,
		Kind:       "DockerMachine",
		Name:       dm.ObjectMeta.Name,
		Namespace:  dm.Namespace,
	}
	if err := r.Update(ctx, m); err != nil {
		return err
	}
	return nil
}

func (r *NodeReconciler) ensureDockerMachineForNode(ctx context.Context, n *corev1.Node) error {
	log := r.Log.WithValues("node", n.Name)

	annotations := n.GetAnnotations()
	if _, ok := annotations[infrav1.NodeOwnerLabelName]; ok {
		return nil
	}
	machines := &infrav1.DockerMachineList{}
	if err := r.List(ctx, machines); err != nil {
		return err
	}
	for _, m := range machines.Items {
		if m.Spec.ProviderID != "" && m.Spec.ProviderID == n.Spec.ProviderID {
			log.V(1).Info("node already has a machine associated with it, only needs an annotation")
			return r.setDockerMachineAnnotation(ctx, &m, n.Name)
		}
	}
	clusterName, err := r.findNodeCluster(n.Name)
	if err != nil {
		return err
	}
	dm := &infrav1.DockerMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      n.Name,
			Namespace: metav1.NamespaceSystem,
		},
		Spec: infrav1.DockerMachineSpec{
			ProviderID:    fmt.Sprintf("docker://%s", n.Name),
			ClusterName:   clusterName,
			ContainerName: n.Name,
			Image:         "",
		},
	}
	if err := r.Create(ctx, dm); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	nodes, err := cinderapi.ListNodes(clusterName)
	if err != nil {
		return err
	}
	addresses := make([]machinev1.MachineAddress, 0)
	for _, node := range nodes {
		if node.String() == n.Name {
			addresses = append(addresses, machinev1.MachineAddress{
				Type:    machinev1.MachineInternalIP,
				Address: node.IP(),
			})
			break
		}
	}
	dm.Status.Addresses = addresses
	dm.Status.Ready = true
	if err := r.Status().Update(ctx, dm); err != nil {
		return err
	}
	return r.setDockerMachineAnnotation(ctx, dm, n.Name)
}

func (r *NodeReconciler) findNodeCluster(nodeName string) (string, error) {
	provider := cluster.NewProvider(cluster.ProviderWithDocker())
	clusters, err := provider.List()
	if err != nil {
		return "", err
	}
	for _, c := range clusters {
		nodes, err := cinderapi.ListNodes(c)
		if err != nil {
			return "", err
		}
		for _, n := range nodes {
			if n.String() == nodeName {
				return c, nil
			}
		}
	}
	return "", fmt.Errorf("could not find cluster for node %q", nodeName)
}

func (r *NodeReconciler) setDockerMachineAnnotation(ctx context.Context, m *infrav1.DockerMachine, name string) error {
	ref := corev1.ObjectReference{
		APIVersion: infrav1.GroupVersion.String(),
		Kind:       "DockerMachine",
		Name:       m.ObjectMeta.Name,
		Namespace:  m.Namespace,
	}
	data, err := json.Marshal(ref)
	if err != nil {
		return err
	}
	k, err := kubernetes.NewForConfig(r.config)
	if err != nil {
		return err
	}
	return nodeutil.PatchNode(ctx, k, name, func(n *corev1.Node) {
		annotations := n.GetAnnotations()
		annotations[infrav1.NodeOwnerLabelName] = string(data)
	})
}
