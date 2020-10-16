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

package v1alpha1

import (
	mapierrors "github.com/criticalstack/machine-api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	MachineFinalizer = "dockermachine.infrastructure.crit.sh"
)

// DockerMachineSpec defines the desired state of DockerMachine
type DockerMachineSpec struct {
	ProviderID    string `json:"providerID,omitempty"`
	ClusterName   string `json:"clusterName"`
	ContainerName string `json:"containerName"`
	Image         string `json:"image"`
}

// DockerMachineStatus defines the observed state of DockerMachine
type DockerMachineStatus struct {
	Ready bool `json:"ready"`
	// FailureReason will be set in the event that there is a terminal problem
	// reconciling the Machine and will contain a succinct value suitable
	// for machine interpretation.
	//
	// This field should not be set for transitive errors that a controller
	// faces that are expected to be fixed automatically over
	// time (like service outages), but instead indicate that something is
	// fundamentally wrong with the Machine's spec or the configuration of
	// the controller, and that manual intervention is required. Examples
	// of terminal errors would be invalid combinations of settings in the
	// spec, values that are unsupported by the controller, or the
	// responsible controller itself being critically misconfigured.
	//
	// Any transient errors that occur during the reconciliation of Machines
	// can be added as events to the Machine object and/or logged in the
	// controller's output.
	// +optional
	FailureReason *mapierrors.MachineStatusError `json:"failureReason,omitempty"`

	// FailureMessage will be set in the event that there is a terminal problem
	// reconciling the Machine and will contain a more verbose string suitable
	// for logging and human consumption.
	//
	// This field should not be set for transitive errors that a controller
	// faces that are expected to be fixed automatically over
	// time (like service outages), but instead indicate that something is
	// fundamentally wrong with the Machine's spec or the configuration of
	// the controller, and that manual intervention is required. Examples
	// of terminal errors would be invalid combinations of settings in the
	// spec, values that are unsupported by the controller, or the
	// responsible controller itself being critically misconfigured.
	//
	// Any transient errors that occur during the reconciliation of Machines
	// can be added as events to the Machine object and/or logged in the
	// controller's output.
	// +optional
	FailureMessage *string `json:"failureMessage,omitempty"`
}

func (m *DockerMachineStatus) SetFailure(err mapierrors.MachineStatusError, msg string) {
	m.FailureReason = &err
	m.FailureMessage = &msg
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=dockermachines,scope=Namespaced,categories=machine-api
// +kubebuilder:storageversion
// +kubebuilder:subresource:status

// DockerMachine is the Schema for the dockermachines API
type DockerMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DockerMachineSpec   `json:"spec,omitempty"`
	Status DockerMachineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DockerMachineList contains a list of DockerMachine
type DockerMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DockerMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DockerMachine{}, &DockerMachineList{})
}
