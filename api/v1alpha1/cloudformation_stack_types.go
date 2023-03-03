// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package v1alpha1

import (
	"fmt"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	CloudFormationStackKind      = "CloudFormationStack"
	CloudFormationStackFinalizer = "finalizers.cloudformation.contrib.fluxcd.io"
	GitRepositoryIndexKey        = ".metadata.gitRepository"
	BucketIndexKey               = ".metadata.bucket"
	OCIRepositoryIndexKey        = ".metadata.ociRepository"
	DefaultTemplatePath          = "template.yaml"
)

// CloudFormationStackSpec defines the desired state of a CloudFormation stack
type CloudFormationStackSpec struct {
	// Name of the CloudFormation stack.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +required
	StackName string `json:"stackName,omitempty"`

	// AWS Region for the CloudFormation stack.
	// +required
	Region string `json:"region,omitempty"`

	// Path to the CloudFormation template file.
	// Defaults to 'None', which translates to the root path of the SourceRef and filename 'template.yaml'.
	// +optional
	TemplatePath string `json:"templatePath,omitempty"`

	// SourceRef is the reference of the source where the CloudFormation template is stored.
	// +required
	SourceRef SourceReference `json:"sourceRef"`

	// The interval at which to reconcile the CloudFormation stack.
	// +required
	Interval metav1.Duration `json:"interval"`

	// The interval at which to poll CloudFormation for the stack's status while a stack
	// action like Create or Update is in progress.
	// When not specified, the controller uses the CloudFormationStackSpec.Interval
	// value to poll the stack.
	// +optional
	PollInterval *metav1.Duration `json:"pollInterval,omitempty"`

	// The interval at which to retry a previously failed reconciliation.
	// When not specified, the controller uses the CloudFormationStackSpec.Interval
	// value to retry failures.
	// +optional
	RetryInterval *metav1.Duration `json:"retryInterval,omitempty"`

	// Suspend tells the controller to suspend reconciliation for this CloudFormation stack,
	// it does not apply to already started reconciliations. Defaults to false.
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// Delete the CloudFormation stack and its underlying resources
	// upon deletion of this object. Defaults to false.
	// +kubebuilder:default:=false
	// +optional
	DestroyStackOnDeletion bool `json:"destroyStackOnDeletion,omitempty"`

	// DependsOn may contain a meta.NamespacedObjectReference slice with
	// references to CloudFormationStack resources that must be ready before this CloudFormationStack
	// can be reconciled.
	// +optional
	DependsOn []meta.NamespacedObjectReference `json:"dependsOn,omitempty"`
}

// Reference to a Flux source object.
type SourceReference struct {
	// API version of the source object.
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`

	// Kind of the source object.
	// +kubebuilder:validation:Enum=GitRepository;Bucket;OCIRepository
	// +required
	Kind string `json:"kind"`

	// Name of the source object.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name"`

	// Namespace of the source object, defaults to the namespace of the CloudFormation stack object.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Optional
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

func (s *SourceReference) String() string {
	if s.Namespace != "" {
		return fmt.Sprintf("%s/%s/%s", s.Kind, s.Namespace, s.Name)
	}
	return fmt.Sprintf("%s/%s", s.Kind, s.Name)
}

// CloudFormationStackStatus defines the observed state of a CloudFormation stack
type CloudFormationStackStatus struct {
	// ObservedGeneration is the last observed generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	meta.ReconcileRequestStatus `json:",inline"`

	// Conditions holds the conditions for the CloudFormationStack.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastAppliedRevision is the revision of the last successfully applied source.
	// The revision format for Git sources is <branch|tag>/<commit-sha>.
	// +optional
	LastAppliedRevision string `json:"lastAppliedRevision,omitempty"`

	// LastAttemptedRevision is the revision of the last reconciliation attempt.
	// +optional
	LastAttemptedRevision string `json:"lastAttemptedRevision,omitempty"`

	// StackName is the name of the CloudFormation stack created by
	// the controller for the CloudFormationStack resource.
	// +optional
	StackName string `json:"stackName,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=cfnstack
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description=""
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""

// CloudFormationStack is the Schema for the CloudFormation stack API
type CloudFormationStack struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CloudFormationStackSpec   `json:"spec,omitempty"`
	Status CloudFormationStackStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CloudFormationStackList contains a list of CloudFormation stacks
type CloudFormationStackList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudFormationStack `json:"items"`
}

// The potential reasons that are associated with condition types
const (
	ArtifactFailedReason          = "ArtifactFailed"
	DependencyNotReadyReason      = "DependencyNotReady"
	ReconciliationSucceededReason = "ReconciliationSucceededReason"
)

// SetCloudFormationStackReadiness sets the ReadyCondition, ObservedGeneration, and LastAttemptedRevision
// on the CloudFormation stack.
func SetCloudFormationStackReadiness(cfnStack *CloudFormationStack, status metav1.ConditionStatus, reason, message string, revision string) {
	newCondition := metav1.Condition{
		Type:    meta.ReadyCondition,
		Status:  status,
		Reason:  reason,
		Message: message,
	}

	apimeta.SetStatusCondition(cfnStack.GetStatusConditions(), newCondition)
	cfnStack.Status.ObservedGeneration = cfnStack.Generation
	if revision != "" {
		cfnStack.Status.LastAttemptedRevision = revision
	}
}

// CloudFormationStackProgressing resets the conditions of the given CloudFormation
// stack to a single ReadyCondition with status ConditionUnknown.
func CloudFormationStackProgressing(cfnStack CloudFormationStack, message string) CloudFormationStack {
	newCondition := metav1.Condition{
		Type:    meta.ReadyCondition,
		Status:  metav1.ConditionUnknown,
		Reason:  meta.ProgressingReason,
		Message: message,
	}
	apimeta.SetStatusCondition(cfnStack.GetStatusConditions(), newCondition)
	return cfnStack
}

// CloudFormationStackNotReady registers a failed reconciliation attempt of the given CloudFormation stack.
func CloudFormationStackNotReady(cfnStack CloudFormationStack, revision, reason, message string) CloudFormationStack {
	SetCloudFormationStackReadiness(
		&cfnStack,
		metav1.ConditionFalse,
		reason,
		message,
		revision,
	)
	return cfnStack
}

// CloudFormationStackReady registers a successful reconciliation of the given CloudFormation stack.
func CloudFormationStackReady(cfnStack CloudFormationStack) CloudFormationStack {
	SetCloudFormationStackReadiness(
		&cfnStack,
		metav1.ConditionTrue,
		ReconciliationSucceededReason,
		"Release reconciliation succeeded",
		cfnStack.Status.LastAttemptedRevision,
	)
	cfnStack.Status.LastAppliedRevision = cfnStack.Status.LastAttemptedRevision
	return cfnStack
}

// GetDependsOn returns the list of dependencies, namespace scoped.
func (in CloudFormationStack) GetDependsOn() []meta.NamespacedObjectReference {
	return in.Spec.DependsOn
}

// GetTemplatePath returns the path to the CloudFormation stack template
func (in CloudFormationStack) GetTemplatePath() string {
	if in.Spec.TemplatePath != "" {
		return in.Spec.TemplatePath
	}
	return DefaultTemplatePath
}

// GetPollInterval returns the poll interval
func (in CloudFormationStack) GetPollInterval() time.Duration {
	if in.Spec.PollInterval != nil {
		return in.Spec.PollInterval.Duration
	}
	return in.Spec.Interval.Duration
}

// GetRetryInterval returns the retry interval
func (in CloudFormationStack) GetRetryInterval() time.Duration {
	if in.Spec.RetryInterval != nil {
		return in.Spec.RetryInterval.Duration
	}
	return in.Spec.Interval.Duration
}

func (in *CloudFormationStack) GetStatusConditions() *[]metav1.Condition {
	return &in.Status.Conditions
}

func init() {
	SchemeBuilder.Register(&CloudFormationStack{}, &CloudFormationStackList{})
}
