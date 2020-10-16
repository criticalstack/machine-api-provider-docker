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
	"fmt"
	"time"

	nodeutil "github.com/criticalstack/crit/pkg/kubernetes/util/node"
	machinev1 "github.com/criticalstack/machine-api/api/v1alpha1"
	mapierrors "github.com/criticalstack/machine-api/errors"
	"github.com/criticalstack/machine-api/util"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrav1 "github.com/criticalstack/machine-api-provider-docker/api/v1alpha1"
	"github.com/criticalstack/machine-api-provider-docker/internal/docker"
)

// DockerMachineReconciler reconciles a DockerMachine object
type DockerMachineReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	config *rest.Config
}

func (r *DockerMachineReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	r.config = mgr.GetConfig()
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(options).
		For(&infrav1.DockerMachine{}).
		Watches(
			&source.Kind{Type: &machinev1.Machine{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: util.MachineToInfrastructureMapFunc(infrav1.GroupVersion.WithKind("DockerMachine")),
			},
		).
		Complete(r)
}

// +kubebuilder:rbac:groups=infrastructure.crit.sh,resources=dockermachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.crit.sh,resources=dockermachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=machine.crit.sh,resources=machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=machine.crit.sh,resources=configs;configs/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;create;update;patch;delete

func (r *DockerMachineReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {
	ctx := context.Background()
	log := r.Log.WithValues("dockermachine", req.NamespacedName)

	dm := &infrav1.DockerMachine{}
	if err := r.Get(ctx, req.NamespacedName, dm); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deleted machines
	if !dm.ObjectMeta.DeletionTimestamp.IsZero() {
		if err := docker.DeleteMachine(ctx, dm); err != nil {
			log.Error(err, "cannot delete node, may already be deleted")
		}
		controllerutil.RemoveFinalizer(dm, infrav1.MachineFinalizer)
		if err := r.Update(ctx, dm); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	m, err := util.GetOwnerMachine(ctx, r.Client, dm.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if m == nil {
		log.Info("Machine Controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("machine", m.Name)

	// Patch any changes to Machine object on each reconciliation.
	patchHelper, err := patch.NewHelper(dm, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer func() {
		if err := patchHelper.Patch(ctx, dm); err != nil {
			if reterr == nil {
				reterr = err
			}
		}
	}()

	// If the DockerMachine doesn't have a finalizer, add one.
	controllerutil.AddFinalizer(dm, infrav1.MachineFinalizer)

	if dm.Spec.ProviderID == "" {
		dm.Spec.ProviderID = fmt.Sprintf("docker://%s", dm.Spec.ContainerName)
	}

	cfg := &machinev1.Config{}
	if err := r.Get(ctx, client.ObjectKey{Name: m.Spec.ConfigRef.Name, Namespace: m.Namespace}, cfg); err != nil {
		return ctrl.Result{}, err
	}

	// TODO(chrism): add label hash of spec (needs config and infra ref fields)
	// and diff to determine if the machine should be replaced
	ok, err := docker.MachineExists(ctx, dm)
	if err != nil {
		return ctrl.Result{}, err
	}
	if ok {
		log.Info("machine already exists")
		return ctrl.Result{}, nil
	}

	if err := docker.CreateMachine(ctx, dm, cfg); err != nil {
		dm.Status.SetFailure(mapierrors.CreateMachineError, err.Error())
		return ctrl.Result{}, err
	}
	client, err := kubernetes.NewForConfig(r.config)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := nodeutil.PatchNode(ctx, client, dm.Spec.ContainerName, func(n *corev1.Node) {
		n.Spec.ProviderID = dm.Spec.ProviderID
	}); err != nil {
		return ctrl.Result{}, mapierrors.NewRequeueErrorf(time.Second, "failed to patch the Kubernetes node with the machine providerID")
	}
	dm.Status.Ready = true
	return ctrl.Result{}, nil
}
