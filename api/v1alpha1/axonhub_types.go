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
// Exactly one of Address and AddressSecret must be set.
// +kubebuilder:validation:XValidation:rule="(has(self.address) && !has(self.addressSecret)) || (!has(self.address) && has(self.addressSecret))",message="exactly one of address or addressSecret must be set"
type DatabaseSpec struct {
	// Type is the database dialect AxonHub should use.
	// +kubebuilder:validation:Enum=sqlite3;postgres;mysql
	Type string `json:"type"`

	// Address is the data source name (DSN) for the selected database
	// type, e.g. "file:axonhub.db?cache=shared&_fk=1" for sqlite3 or
	// "postgres://host:5432/axonhub?sslmode=disable" for postgres.
	// Use AddressSecret instead when the DSN contains credentials, so they
	// are not stored in the AxonHub resource.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	Address string `json:"address,omitempty"`

	// AddressSecret references a key in a Secret in the AxonHub resource's
	// namespace whose value is the data source name (DSN) for the selected
	// database type. The controller injects it through an
	// env.valueFrom.secretKeyRef, so the credentials never live in the
	// AxonHub resource. Mutually exclusive with Address.
	AddressSecret *SecretKeySelector `json:"addressSecret,omitempty"`
}

// SecretKeySelector selects a key of a Secret in the same namespace as the
// AxonHub resource.
type SecretKeySelector struct {
	// Name is the name of the Secret.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern="^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$"
	Name string `json:"name"`

	// Key is the key within the Secret whose value is the DSN.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern="^[-._a-zA-Z0-9]+$"
	Key string `json:"key"`
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
