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

	"github.com/go-logr/logr"
	"github.com/go-openapi/spec"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/criticalstack/machine-api-provider-docker/api/v1alpha1"
)

const OpenAPISchemaSecretName = "config-schema"

// DockerMachineReconciler reconciles a DockerMachine object
type DockerInfrastructureProviderReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	config *rest.Config
}

func (r *DockerInfrastructureProviderReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	r.config = mgr.GetConfig()
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.DockerInfrastructureProvider{}).
		Owns(&v1.Secret{}).
		WithOptions(options).
		Complete(r)
}

var schema = spec.Schema{
	SchemaProps: spec.SchemaProps{
		Type:  spec.StringOrArray{"object"},
		Title: "Docker Worker Config",
		Properties: map[string]spec.Schema{
			"apiVersion": {
				SchemaProps: spec.SchemaProps{
					Type:    spec.StringOrArray{"string"},
					Default: v1alpha1.GroupVersion.String(),
				},
			},
			"kind": {
				SchemaProps: spec.SchemaProps{
					Type:    spec.StringOrArray{"string"},
					Default: "DockerMachine",
				},
			},
			"metadata": {
				SchemaProps: spec.SchemaProps{
					Type:  spec.StringOrArray{"object"},
					Title: "Metadata",
					Properties: map[string]spec.Schema{
						"name": {
							SchemaProps: spec.SchemaProps{
								Type: spec.StringOrArray{"string"},
							},
						},
					},
					Required: []string{"name"},
				},
			},
			"spec": {
				SchemaProps: spec.SchemaProps{
					Type:  spec.StringOrArray{"object"},
					Title: "Docker Worker Config",
					Properties: map[string]spec.Schema{
						"image": {
							SchemaProps: spec.SchemaProps{
								Type:        spec.StringOrArray{"string"},
								Description: "container image to use",
								Default:     "criticalstack/cinder:v1.0.0-beta.10",
							},
						},
						"containerName": {
							SchemaProps: spec.SchemaProps{
								Type:        spec.StringOrArray{"string"},
								Description: "container name",
								Default:     "cinder-worker",
							},
						},
						"clusterName": {
							SchemaProps: spec.SchemaProps{
								Type:        spec.StringOrArray{"string"},
								Description: "cluster name",
								Default:     "cinder",
							},
						},
					},
					Required: []string{"image", "clusterName"},
				},
			},
		},
		Required: []string{"apiVersion", "kind"},
	},
}

// +kubebuilder:rbac:groups=infrastructure.crit.sh,resources=dockerinfrastructureproviders,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.crit.sh,resources=dockerinfrastructureproviders/status,verbs=create;update
// +kubebuilder:rbac:groups=,resources=secrets,verbs=*

func (r *DockerInfrastructureProviderReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {
	ctx := context.Background()
	log := r.Log.WithValues("dockerinfrastructureprovider", req.NamespacedName)

	ip := &v1alpha1.DockerInfrastructureProvider{}
	if err := r.Get(ctx, req.NamespacedName, ip); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	var s corev1.Secret
	s.SetName(OpenAPISchemaSecretName)
	s.SetNamespace(ip.Namespace)
	if err := r.Get(ctx, client.ObjectKey{Name: s.Name, Namespace: s.Namespace}, &s); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	ip.Status.Ready = !s.GetCreationTimestamp().Time.IsZero() // ready if secret already exists
	ip.Status.LastUpdated = metav1.Now()
	defer func() {
		if err := r.Status().Update(ctx, ip); err != nil {
			log.Error(err, "failed to update provider status")
		}
	}()
	b, err := json.Marshal(schema)
	if err != nil {
		return ctrl.Result{}, err
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, &s, func() error {
		s.Data = map[string][]byte{"schema": b}
		return controllerutil.SetControllerReference(ip, &s, r.Scheme)
	}); err != nil {
		return ctrl.Result{}, err
	}

	ip.Status.Ready = true
	return ctrl.Result{}, nil
}
