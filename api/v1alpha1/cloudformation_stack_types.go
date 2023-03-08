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
	// Defaults to the root path of the SourceRef and filename 'template.yaml'.
	// +optional
	// +kubebuilder:default="template.yaml"
	TemplatePath string `json:"templatePath,omitempty"`

	// SourceRef is the reference of the source where the CloudFormation template is stored.
	// +required
	SourceRef SourceReference `json:"sourceRef"`

	// The interval at which to reconcile the CloudFormation stack.
	// +required
	Interval metav1.Duration `json:"interval"`

	// The interval at which to poll CloudFormation for the stack's status while a stack
	// action like Create or Update is in progress.
	// Defaults to five seconds.
	// +optional
	// +kubebuilder:default="5s"
	PollInterval metav1.Duration `json:"pollInterval,omitempty"`

	// The interval at which to retry a previously failed reconciliation.
	// When not specified, the controller uses the CloudFormationStackSpec.Interval
	// value to retry failures.
	// +optional
	RetryInterval *metav1.Duration `json:"retryInterval,omitempty"`

	// Suspend tells the controller to suspend reconciliation for this CloudFormation stack,
	// it does not apply to already started reconciliations. Defaults to false.
	// +optional
	// +kubebuilder:default:=false
	Suspend bool `json:"suspend,omitempty"`

	// Delete the CloudFormation stack and its underlying resources
	// upon deletion of this object. Defaults to false.
	// +optional
	// +kubebuilder:default:=false
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

	// LastAppliedChangeSet is the ARN of the last successfully applied CloudFormation change set.
	// +optional
	LastAppliedChangeSet string `json:"lastAppliedChangeSet,omitempty"`

	// LastAttemptedChangeSet is the ARN of the CloudFormation change set for the last reconciliation attempt.
	// +optional
	LastAttemptedChangeSet string `json:"lastAttemptedChangeSet,omitempty"`

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

// The potential reasons that are associated with the condition types
const (
	ArtifactFailedReason              = "ArtifactFailed"
	ChangeSetFailedReason             = "ChangeSetFailed"
	CloudFormationApiCallFailedReason = "CloudFormationApiCallFailed"
	UnrecoverableStackFailureReason   = "UnrecoverableStackFailure"
	DependencyNotReadyReason          = "DependencyNotReady"
	UnexpectedStatusReason            = "UnexpectedStatus"
)

type ReadinessUpdate struct {
	Reason         string
	Message        string
	SourceRevision string
	ChangeSetArn   string
}

// SetCloudFormationStackReadiness sets the ReadyCondition, ObservedGeneration, LastAttemptedChangeSet, and LastAttemptedRevision
// on the CloudFormation stack.
func SetCloudFormationStackReadiness(cfnStack *CloudFormationStack, status metav1.ConditionStatus, update ReadinessUpdate) {
	newCondition := metav1.Condition{
		Type:    meta.ReadyCondition,
		Status:  status,
		Reason:  update.Reason,
		Message: update.Message,
	}

	apimeta.SetStatusCondition(cfnStack.GetStatusConditions(), newCondition)
	cfnStack.Status.ObservedGeneration = cfnStack.Generation
	cfnStack.Status.StackName = cfnStack.Spec.StackName
	if update.SourceRevision != "" {
		cfnStack.Status.LastAttemptedRevision = update.SourceRevision
	}
	if update.ChangeSetArn != "" {
		cfnStack.Status.LastAttemptedChangeSet = update.ChangeSetArn
	}
}

// CloudFormationStackProgressing resets the conditions of the given CloudFormation
// stack to a single ReadyCondition with status ConditionUnknown.
func CloudFormationStackProgressing(cfnStack CloudFormationStack, update ReadinessUpdate) CloudFormationStack {
	update.Reason = meta.ProgressingReason
	SetCloudFormationStackReadiness(&cfnStack, metav1.ConditionUnknown, update)
	return cfnStack
}

// CloudFormationStackNotReady registers a failed reconciliation attempt of the given CloudFormation stack.
func CloudFormationStackNotReady(cfnStack CloudFormationStack, update ReadinessUpdate) CloudFormationStack {
	SetCloudFormationStackReadiness(&cfnStack, metav1.ConditionFalse, update)
	return cfnStack
}

// CloudFormationStackReady registers a successful reconciliation of the given CloudFormation stack.
func CloudFormationStackReady(cfnStack CloudFormationStack, changeSetArn string) CloudFormationStack {
	SetCloudFormationStackReadiness(
		&cfnStack,
		metav1.ConditionTrue,
		ReadinessUpdate{
			Reason:         meta.SucceededReason,
			Message:        "Stack reconciliation succeeded",
			SourceRevision: cfnStack.Status.LastAttemptedRevision,
			ChangeSetArn:   changeSetArn,
		},
	)
	cfnStack.Status.LastAppliedRevision = cfnStack.Status.LastAttemptedRevision
	cfnStack.Status.LastAppliedChangeSet = cfnStack.Status.LastAttemptedChangeSet
	return cfnStack
}

// GetDependsOn returns the list of dependencies, namespace scoped.
func (in CloudFormationStack) GetDependsOn() []meta.NamespacedObjectReference {
	return in.Spec.DependsOn
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
