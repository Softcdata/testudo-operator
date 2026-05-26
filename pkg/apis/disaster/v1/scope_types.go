/*
Copyright 2025.

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

package v1

import (
	"k8s.io/apimachinery/pkg/types"
)

// ScopeType defines the scope of the statistics
// +kubebuilder:validation:Enum=app;namespace;cluster;custom
type ScopeType string

const (
	ScopeTypeApp       ScopeType = "app"
	ScopeTypeNamespace ScopeType = "namespace"
	ScopeTypeCluster   ScopeType = "cluster"
	ScopeTypeCustom    ScopeType = "custom"
)

// ScopeReference defines the reference to the scope object
type ScopeReference struct {
	// APIVersion of the referent.
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`

	// Kind of the referent.
	// +optional
	Kind string `json:"kind,omitempty"`

	// Name of the referent.
	// +optional
	Name string `json:"name,omitempty"`

	// Namespace of the referent.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// UID of the referent.
	// +optional
	UID types.UID `json:"uid,omitempty"`
}
