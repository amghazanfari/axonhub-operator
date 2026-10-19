/*
Copyright 2026.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AxonHubSpec defines the desired state of AxonHub.
type AxonHubSpec struct {
	// Replicas is the desired number of AxonHub pods.
	// The minimum is intentionally 1 (not 0): the operator does not manage a
	// drain/maintenance workflow that would require scale-to-zero, and with
	// the default local SQLite storage (see spec.db) a pod must be running
	// for AxonHub to stay reachable. Removing the workload entirely should
	// be done by deleting the AxonHub resource, which garbage-collects it.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Image is the AxonHub container image (repository[:tag]).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:default="looplj/axonhub:latest"
	Image string `json:"image,omitempty"`

	// DB configures the AxonHub database. When omitted, AxonHub's own
	// defaults apply (SQLite with a local file, which is lost when the
	// pod's filesystem is replaced).
	DB *DatabaseSpec `json:"db,omitempty"`
}

// DatabaseSpec selects which of the AxonHub-supported databases to use and
// how to reach it. It maps to the AXONHUB_DB_DIALECT and AXONHUB_DB_DSN
// environment variables of the AxonHub container.
type DatabaseSpec struct {
	// Type is the database dialect AxonHub should use.
	// +kubebuilder:validation:Enum=sqlite3;postgres;mysql
	Type string `json:"type"`

	// Address is the data source name (DSN) for the selected database
	// type, e.g. "file:axonhub.db?cache=shared&_fk=1" for sqlite3 or
	// "postgres://user:pass@host:5432/axonhub?sslmode=disable" for
	// postgres.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	Address string `json:"address"`
}

// AxonHubStatus defines the observed state of AxonHub.
type AxonHubStatus struct {
	// ObservedGeneration is the generation most recently observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ReadyReplicas is the number of pods of the managed Deployment reported ready.
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Conditions represent the latest available observations of the resource's state.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AxonHub is the Schema for the axonhubs API.
type AxonHub struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AxonHubSpec   `json:"spec,omitempty"`
	Status AxonHubStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AxonHubList contains a list of AxonHub.
type AxonHubList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AxonHub `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AxonHub{}, &AxonHubList{})
}
