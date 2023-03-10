// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package controllers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-retryablehttp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kuberecorder "k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/reference"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/metrics"
	"github.com/fluxcd/pkg/runtime/predicates"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"

	cfnv1 "github.com/awslabs/aws-cloudformation-controller-for-flux/api/v1alpha1"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/cloudformation"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/s3"
)

//+kubebuilder:rbac:groups=cloudformation.contrib.fluxcd.io,resources=cloudformationstacks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cloudformation.contrib.fluxcd.io,resources=cloudformationstacks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cloudformation.contrib.fluxcd.io,resources=cloudformationstacks/finalizers,verbs=get;create;update;patch;delete
//+kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=buckets;gitrepositories;ocirepositories,verbs=get;list;watch
//+kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=buckets/status;gitrepositories/status;ocirepositories/status,verbs=get
//+kubebuilder:rbac:groups="",resources=configmaps;secrets;serviceaccounts,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// CloudFormationStackReconciler reconciles a CloudFormationStack object
type CloudFormationStackReconciler struct {
	client.Client
	httpClient        *retryablehttp.Client
	requeueDependency time.Duration

	CfnClient       *cloudformation.CloudFormation
	S3Client        *s3.S3
	EventRecorder   kuberecorder.EventRecorder
	MetricsRecorder *metrics.Recorder
	Scheme          *runtime.Scheme
}

type CloudFormationStackReconcilerOptions struct {
	MaxConcurrentReconciles int
	HTTPRetry               int
}

func (r *CloudFormationStackReconciler) SetupWithManager(mgr ctrl.Manager, opts CloudFormationStackReconcilerOptions) error {
	// Index the CloudFormationStacks by their source references
	if err := mgr.GetCache().IndexField(context.TODO(), &cfnv1.CloudFormationStack{}, cfnv1.GitRepositoryIndexKey,
		r.IndexBy(sourcev1.GitRepositoryKind)); err != nil {
		return fmt.Errorf("failed setting index fields: %w", err)
	}
	if err := mgr.GetCache().IndexField(context.TODO(), &cfnv1.CloudFormationStack{}, cfnv1.BucketIndexKey,
		r.IndexBy(sourcev1.BucketKind)); err != nil {
		return fmt.Errorf("failed setting index fields: %w", err)
	}
	if err := mgr.GetCache().IndexField(context.TODO(), &cfnv1.CloudFormationStack{}, cfnv1.OCIRepositoryIndexKey,
		r.IndexBy(sourcev1.OCIRepositoryKind)); err != nil {
		return fmt.Errorf("failed setting index fields: %w", err)
	}

	// Configure the retryable http client for retrieving artifacts.
	httpClient := retryablehttp.NewClient()
	httpClient.RetryWaitMin = 5 * time.Second
	httpClient.RetryWaitMax = 30 * time.Second
	httpClient.RetryMax = opts.HTTPRetry
	httpClient.Logger = nil
	r.httpClient = httpClient

	// Watch for source object changes and CloudFormation stack object changes
	return ctrl.NewControllerManagedBy(mgr).
		For(&cfnv1.CloudFormationStack{}, builder.WithPredicates(
			predicate.Or(predicate.GenerationChangedPredicate{}, predicates.ReconcileRequestedPredicate{}),
		)).
		Watches(
			&source.Kind{Type: &sourcev1.GitRepository{}},
			handler.EnqueueRequestsFromMapFunc(r.requestsForRevisionChangeOf(cfnv1.GitRepositoryIndexKey)),
			builder.WithPredicates(SourceRevisionChangePredicate{}),
		).
		Watches(
			&source.Kind{Type: &sourcev1.Bucket{}},
			handler.EnqueueRequestsFromMapFunc(r.requestsForRevisionChangeOf(cfnv1.BucketIndexKey)),
			builder.WithPredicates(SourceRevisionChangePredicate{}),
		).
		Watches(
			&source.Kind{Type: &sourcev1.OCIRepository{}},
			handler.EnqueueRequestsFromMapFunc(r.requestsForRevisionChangeOf(cfnv1.OCIRepositoryIndexKey)),
			builder.WithPredicates(SourceRevisionChangePredicate{}),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: opts.MaxConcurrentReconciles,
			RecoverPanic:            pointer.Bool(true),
		}).
		Complete(r)
}

func (r *CloudFormationStackReconciler) IndexBy(kind string) func(o client.Object) []string {
	return func(o client.Object) []string {
		stack := o.(*cfnv1.CloudFormationStack)
		if stack.Spec.SourceRef.Kind == kind {
			namespace := stack.GetNamespace()
			// default to the stack's namespace
			if stack.Spec.SourceRef.Namespace != "" {
				namespace = stack.Spec.SourceRef.Namespace
			}
			return []string{fmt.Sprintf("%s/%s", namespace, stack.Spec.SourceRef.Name)}
		}

		return nil
	}
}

func (r *CloudFormationStackReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	start := time.Now()
	log := ctrl.LoggerFrom(ctx)

	var cfnStack cfnv1.CloudFormationStack
	if err := r.Get(ctx, req.NamespacedName, &cfnStack); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	defer r.recordSuspension(ctx, cfnStack)

	// Add our finalizer if it does not exist
	if !controllerutil.ContainsFinalizer(&cfnStack, cfnv1.CloudFormationStackFinalizer) {
		patch := client.MergeFrom(cfnStack.DeepCopy())
		controllerutil.AddFinalizer(&cfnStack, cfnv1.CloudFormationStackFinalizer)
		if err := r.Patch(ctx, &cfnStack, patch); err != nil {
			log.Error(err, "Unable to register finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the CloudFormationStack is suspended
	if cfnStack.Spec.Suspend {
		log.Info("Reconciliation is suspended for this object")
		return ctrl.Result{}, nil
	}

	// Check if the object is being deleted
	if !cfnStack.ObjectMeta.DeletionTimestamp.IsZero() {
		cfnStack, result, err := r.reconcileDelete(ctx, cfnStack)

		// Update status
		// Skip updating the status if the stack is successfully deleted (no requeueAfter set).
		// The finalizer has been removed, so the object is likely gone from the API server already.
		if result.Requeue || result.RequeueAfter > 0 {
			if updateStatusErr := r.patchStatus(ctx, &cfnStack); updateStatusErr != nil {
				log.Error(updateStatusErr, "Unable to update status after delete reconciliation")
				return ctrl.Result{Requeue: true}, updateStatusErr
			}
		}

		durationMsg := fmt.Sprintf("Deletion reconcilation loop finished in %s", time.Now().Sub(start).String())
		if result.RequeueAfter > 0 {
			durationMsg = fmt.Sprintf("%s, next run in %s", durationMsg, result.RequeueAfter.String())
		}
		log.Info(durationMsg)

		return result, err
	}

	// Reconcile
	cfnStack, result, err := r.reconcile(ctx, cfnStack)

	// Update status
	if updateStatusErr := r.patchStatus(ctx, &cfnStack); updateStatusErr != nil {
		log.Error(updateStatusErr, "Unable to update status after reconciliation")
		return ctrl.Result{Requeue: true}, updateStatusErr
	}

	// Record ready status
	r.recordReadiness(ctx, cfnStack)

	// Log reconciliation duration
	durationMsg := fmt.Sprintf("Reconcilation loop finished in %s", time.Now().Sub(start).String())
	if result.RequeueAfter > 0 {
		durationMsg = fmt.Sprintf("%s, next run in %s", durationMsg, result.RequeueAfter.String())
	}
	log.Info(durationMsg)

	return result, err
}

// reconcile creates or updates the CloudFormation stack as needed.
func (r *CloudFormationStackReconciler) reconcile(ctx context.Context, cfnStack cfnv1.CloudFormationStack) (cfnv1.CloudFormationStack, ctrl.Result, error) {
	reconcileStart := time.Now()
	log := ctrl.LoggerFrom(ctx)

	// Record the value of the reconciliation request, if any
	if v, ok := meta.ReconcileAnnotationValue(cfnStack.GetAnnotations()); ok {
		cfnStack.Status.SetLastHandledReconcileRequest(v)
	}

	// Observe CloudFormationStack generation.
	if cfnStack.Status.ObservedGeneration != cfnStack.Generation {
		cfnStack = cfnv1.CloudFormationStackProgressing(cfnStack, cfnv1.ReadinessUpdate{Message: "Stack reconciliation in progress"})
		if updateStatusErr := r.patchStatus(ctx, &cfnStack); updateStatusErr != nil {
			log.Error(updateStatusErr, "Unable to update status after generation update")
			return cfnStack, ctrl.Result{Requeue: true}, updateStatusErr
		}
		// Record progressing status
		r.recordReadiness(ctx, cfnStack)
	}

	// Record reconciliation duration
	if r.MetricsRecorder != nil {
		objRef, err := reference.GetReference(r.Scheme, &cfnStack)
		if err != nil {
			return cfnStack, ctrl.Result{Requeue: true}, err
		}
		defer r.MetricsRecorder.RecordDuration(*objRef, reconcileStart)
	}

	// Resolve source reference
	sourceObj, err := r.getSource(ctx, cfnStack)
	if err != nil {
		if apierrors.IsNotFound(err) {
			msg := fmt.Sprintf("Source '%s' not found", cfnStack.Spec.SourceRef.String())
			cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{Message: msg, Reason: cfnv1.ArtifactFailedReason})
			log.Info(msg)
			// do not requeue immediately, when the source is created the watcher should trigger a reconciliation
			return cfnStack, ctrl.Result{RequeueAfter: cfnStack.GetRetryInterval()}, nil
		} else {
			// retry on transient errors
			msg := fmt.Sprintf("Failed to resolve source '%s'", cfnStack.Spec.SourceRef.String())
			log.Error(err, msg)
			cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{Message: msg, Reason: cfnv1.ArtifactFailedReason})
			return cfnStack, ctrl.Result{Requeue: true}, err
		}
	}

	// Check source readiness
	if sourceObj.GetArtifact() == nil {
		msg := fmt.Sprintf("Source '%s' is not ready, artifact not found", cfnStack.Spec.SourceRef.String())
		cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{Message: msg, Reason: cfnv1.ArtifactFailedReason})
		log.Info(msg)
		// do not requeue immediately, when the artifact is created the watcher should trigger a reconciliation
		return cfnStack, ctrl.Result{RequeueAfter: cfnStack.GetRetryInterval()}, nil
	}

	// Load stack template file from artifact
	templateContents, err := r.loadCloudFormationTemplate(cfnStack, sourceObj.GetArtifact())
	if err != nil {
		msg := fmt.Sprintf("Failed to load template '%s' from source '%s'", cfnStack.Spec.TemplatePath, cfnStack.Spec.SourceRef.String())
		log.Error(err, msg)
		cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{Message: msg, Reason: cfnv1.ArtifactFailedReason})
		return cfnStack, ctrl.Result{RequeueAfter: cfnStack.GetRetryInterval()}, nil
	}
	revision := sourceObj.GetArtifact().Revision

	// Reconcile CloudFormation stack
	reconciledCfnStack, requeueInterval, err := r.reconcileStack(ctx, *cfnStack.DeepCopy(), templateContents, revision)
	if err != nil {
		log.Error(err, "Failed to reconcile stack")
		return reconciledCfnStack, ctrl.Result{RequeueAfter: requeueInterval}, err
	}

	return reconciledCfnStack, ctrl.Result{RequeueAfter: requeueInterval}, nil
}

func (r *CloudFormationStackReconciler) reconcileStack(ctx context.Context, cfnStack cfnv1.CloudFormationStack, templateContents *bytes.Buffer, revision string) (cfnv1.CloudFormationStack, time.Duration, error) {
	log := ctrl.LoggerFrom(ctx)

	// Convert the Flux controller stack type into the CloudFormation client stack type
	clientStack := &cloudformation.Stack{
		Name: cfnStack.Spec.StackName,
		// Region:         cfnStack.Spec.Region,
		Generation:     cfnStack.Generation,
		SourceRevision: revision,
		ChangeSetArn:   cfnStack.Status.LastAttemptedChangeSet,
		StackConfig: &cloudformation.StackConfig{
			// TODO get bucket from annotations, controller flags, etc
			TemplateBucket: os.Getenv("TEMPLATE_BUCKET"),
			TemplateBody:   templateContents.String(),
		},
	}

	// Find the existing stack, if any
	desc, err := r.CfnClient.DescribeStack(clientStack)

	// Check if the stack exists; if not, create it
	if err != nil {
		var e *cloudformation.ErrStackNotFound
		if errors.As(err, &e) {
			return r.reconcileChangeset(ctx, cfnStack, clientStack, revision, true)
		} else {
			msg := fmt.Sprintf("Failed to describe the stack '%s'", clientStack.Name)
			log.Error(err, msg)
			cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg, Reason: cfnv1.CloudFormationApiCallFailedReason})
			return cfnStack, cfnStack.GetRetryInterval(), err
		}
	}

	// Keep polling if the stack is still in progress
	if desc.InProgress() {
		msg := fmt.Sprintf("Stack action for stack '%s' is in progress (status: '%s'), waiting for stack action to complete", clientStack.Name, desc.StackStatus)
		log.Info(msg)
		cfnStack = cfnv1.CloudFormationStackProgressing(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg})
		return cfnStack, cfnStack.Spec.PollInterval.Duration, nil
	}

	// Continue rollback if a previous update rollback failed
	if desc.RequiresRollbackContinuation() {
		if err := r.CfnClient.ContinueRollback(clientStack); err != nil {
			msg := fmt.Sprintf("Failed to continue a failed rollback for stack '%s'", clientStack.Name)
			log.Error(err, msg)
			cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg, Reason: cfnv1.CloudFormationApiCallFailedReason})
			return cfnStack, cfnStack.GetRetryInterval(), err
		}
		// TODO emit a failure event for the recoverable failure, but keep the stack object in 'Progressing' status
		msg := fmt.Sprintf("Stack '%s' has a previously failed update rollback (status '%s'), continuing rollback", clientStack.Name, desc.StackStatus)
		log.Info(msg)
		cfnStack = cfnv1.CloudFormationStackProgressing(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg})
		return cfnStack, cfnStack.Spec.PollInterval.Duration, nil
	}

	// Delete the stack if it has failed to create or delete
	if desc.RequiresCleanup() {
		if err := r.CfnClient.DeleteStack(clientStack); err != nil {
			msg := fmt.Sprintf("Failed to delete the failed stack '%s'", clientStack.Name)
			log.Error(err, msg)
			cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg, Reason: cfnv1.CloudFormationApiCallFailedReason})
			return cfnStack, cfnStack.GetRetryInterval(), err
		}

		msg := fmt.Sprintf("Stack '%s' is in an unrecoverable state and must be recreated: status '%s'", clientStack.Name, desc.StackStatus)
		if desc.StackStatusReason != nil {
			msg = fmt.Sprintf("%s, reason '%s'", msg, *desc.StackStatusReason)
		}
		log.Info(msg)
		cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg, Reason: cfnv1.UnrecoverableStackFailureReason})
		return cfnStack, cfnStack.GetRetryInterval(), nil
	}

	// Check if the stack is ready for an update
	if desc.IsSuccess() || desc.IsRecoverableFailure() {
		// TODO emit a failure event for the recoverable failure, but keep the stack object in 'Progressing' status
		return r.reconcileChangeset(ctx, cfnStack, clientStack, revision, false)
	}

	msg := fmt.Sprintf("Unexpected stack status for stack '%s': status '%s'", clientStack.Name, desc.StackStatus)
	if desc.StackStatusReason != nil {
		msg = fmt.Sprintf("%s, reason '%s'", msg, *desc.StackStatusReason)
	}
	log.Info(msg)
	cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg, Reason: cfnv1.UnexpectedStatusReason})
	return cfnStack, cfnStack.GetRetryInterval(), nil
}

func (r *CloudFormationStackReconciler) reconcileChangeset(ctx context.Context, cfnStack cfnv1.CloudFormationStack, clientStack *cloudformation.Stack, revision string, isCreate bool) (cfnv1.CloudFormationStack, time.Duration, error) {
	log := ctrl.LoggerFrom(ctx)

	desc, err := r.CfnClient.DescribeChangeSet(clientStack)

	// Check if the change set exists; if not, create it.
	// If the change set is empty, we can delete it and declare success
	if err != nil {
		var notFoundErr *cloudformation.ErrChangeSetNotFound
		var emptyErr *cloudformation.ErrChangeSetEmpty
		if errors.As(err, &notFoundErr) {
			err := r.uploadStackTemplate(clientStack)
			if err != nil {
				msg := fmt.Sprintf("Failed to upload template to S3 for stack '%s'", clientStack.Name)
				log.Error(err, msg)
				cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg, Reason: cfnv1.TemplateUploadFailedReason})
				return cfnStack, cfnStack.GetRetryInterval(), err
			}
			log.Info(fmt.Sprintf("Creating a change set for stack '%s' with template '%s'", clientStack.Name, clientStack.TemplateURL))

			if isCreate {
				arn, err := r.CfnClient.CreateStack(clientStack)
				if err != nil {
					msg := fmt.Sprintf("Failed to create a change set for stack '%s'", clientStack.Name)
					log.Error(err, msg)
					cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg, Reason: cfnv1.CloudFormationApiCallFailedReason})
					return cfnStack, cfnStack.GetRetryInterval(), err
				}
				msg := fmt.Sprintf("Stack creation for stack '%s' in progress", clientStack.Name)
				log.Info(msg)
				cfnStack = cfnv1.CloudFormationStackProgressing(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg, ChangeSetArn: arn})
				return cfnStack, cfnStack.Spec.PollInterval.Duration, nil
			} else {
				arn, err := r.CfnClient.UpdateStack(clientStack)
				if err != nil {
					msg := fmt.Sprintf("Failed to create a change set for stack '%s'", clientStack.Name)
					log.Error(err, msg)
					cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg, Reason: cfnv1.CloudFormationApiCallFailedReason})
					return cfnStack, cfnStack.GetRetryInterval(), err
				}
				msg := fmt.Sprintf("Stack update for stack '%s' in progress", clientStack.Name)
				log.Info(msg)
				cfnStack = cfnv1.CloudFormationStackProgressing(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg, ChangeSetArn: arn})
				return cfnStack, cfnStack.Spec.PollInterval.Duration, nil
			}
		} else if errors.As(err, &emptyErr) {
			// This changeset was empty, meaning that the stack is up to date with the latest template
			if err := r.CfnClient.DeleteChangeSet(clientStack); err != nil {
				msg := fmt.Sprintf("Failed to delete an empty change set for stack '%s'", clientStack.Name)
				log.Error(err, msg)
				cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg, Reason: cfnv1.CloudFormationApiCallFailedReason})
				return cfnStack, cfnStack.GetRetryInterval(), err
			}
			// Success!
			log.Info(fmt.Sprintf("Successfully reconciled stack '%s' with change set '%s'", clientStack.Name, emptyErr.Arn))
			return cfnv1.CloudFormationStackReady(cfnStack, emptyErr.Arn), cfnStack.Spec.Interval.Duration, nil
		} else {
			msg := fmt.Sprintf("Failed to describe a change set for stack '%s'", clientStack.Name)
			log.Error(err, msg)
			cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg, Reason: cfnv1.CloudFormationApiCallFailedReason})
			return cfnStack, cfnStack.GetRetryInterval(), err
		}
	}

	// If change set failed, delete it so we can create it again
	if desc.IsFailed() {
		if err := r.CfnClient.DeleteChangeSet(clientStack); err != nil {
			msg := fmt.Sprintf("Failed to delete a failed change set for stack '%s'", clientStack.Name)
			log.Error(err, msg)
			cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{
				ChangeSetArn:   desc.Arn,
				SourceRevision: revision,
				Message:        msg,
				Reason:         cfnv1.CloudFormationApiCallFailedReason,
			})
			return cfnStack, cfnStack.GetRetryInterval(), err
		}

		msg := fmt.Sprintf("Change set failed for stack '%s': status '%s', execution status '%s', reason '%s'", clientStack.Name, desc.Status, desc.ExecutionStatus, desc.StatusReason)
		log.Info(msg)
		cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{
			ChangeSetArn:   desc.Arn,
			SourceRevision: revision,
			Message:        msg,
			Reason:         cfnv1.ChangeSetFailedReason,
		})
		return cfnStack, cfnStack.GetRetryInterval(), nil
	}

	// Keep polling if the change set is still in progress
	if desc.InProgress() {
		msg := fmt.Sprintf("Change set is in progress for stack '%s': status '%s', execution status '%s', reason '%s'", clientStack.Name, desc.Status, desc.ExecutionStatus, desc.StatusReason)
		log.Info(msg)
		cfnStack = cfnv1.CloudFormationStackProgressing(cfnStack, cfnv1.ReadinessUpdate{SourceRevision: revision, Message: msg, ChangeSetArn: desc.Arn})
		return cfnStack, cfnStack.Spec.PollInterval.Duration, nil
	}

	// This changeset was successfully applied, meaning that the stack is up to date with the latest template
	if desc.IsSuccess() {
		// Success!
		log.Info(fmt.Sprintf("Successfully reconciled stack '%s' with change set '%s'", clientStack.Name, desc.Arn))
		return cfnv1.CloudFormationStackReady(cfnStack, desc.Arn), cfnStack.Spec.Interval.Duration, nil
	}

	// Start the change set execution
	if desc.ReadyForExecution() {
		if err := r.CfnClient.ExecuteChangeSet(clientStack); err != nil {
			msg := fmt.Sprintf("Failed to execute a change set for stack '%s'", clientStack.Name)
			log.Error(err, msg)
			cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{
				ChangeSetArn:   desc.Arn,
				SourceRevision: revision,
				Message:        msg,
				Reason:         cfnv1.CloudFormationApiCallFailedReason,
			})
			return cfnStack, cfnStack.GetRetryInterval(), err
		}
		msg := fmt.Sprintf("Change set execution started for stack '%s'", clientStack.Name)
		log.Info(msg)
		cfnStack = cfnv1.CloudFormationStackProgressing(cfnStack, cfnv1.ReadinessUpdate{Message: msg})
		return cfnStack, cfnStack.Spec.PollInterval.Duration, nil
	}

	msg := fmt.Sprintf("Unexpected change set status for stack '%s': status '%s', execution status '%s', reason '%s'", clientStack.Name, desc.Status, desc.ExecutionStatus, desc.StatusReason)
	log.Info(msg)
	cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{
		ChangeSetArn:   desc.Arn,
		SourceRevision: revision,
		Message:        msg,
		Reason:         cfnv1.UnexpectedStatusReason,
	})
	return cfnStack, cfnStack.GetRetryInterval(), nil
}

func (r *CloudFormationStackReconciler) uploadStackTemplate(clientStack *cloudformation.Stack) error {
	id, err := uuid.NewRandom()
	if err != nil {
		return fmt.Errorf("generate random id: %w", err)
	}
	objectKey := fmt.Sprintf("flux-%s-%s.template", clientStack.Name, id.String())

	url, err := r.S3Client.UploadTemplate(
		clientStack.TemplateBucket,
		clientStack.Region,
		objectKey,
		strings.NewReader(clientStack.TemplateBody),
	)
	if err != nil {
		return err
	}

	clientStack.TemplateURL = url
	return nil
}

// reconcileDelete deletes the CloudFormation stack.
func (r *CloudFormationStackReconciler) reconcileDelete(ctx context.Context, cfnStack cfnv1.CloudFormationStack) (cfnv1.CloudFormationStack, ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	r.recordReadiness(ctx, cfnStack)

	if !cfnStack.Spec.Suspend {
		// Convert the Flux controller stack type into the CloudFormation client stack type
		clientStack := &cloudformation.Stack{
			Name: cfnStack.Spec.StackName,
			// Region:         cfnStack.Spec.Region,
			Generation:     cfnStack.Generation,
			SourceRevision: cfnStack.Status.LastAttemptedRevision,
			ChangeSetArn:   cfnStack.Status.LastAttemptedChangeSet,
		}

		// Find the existing stack, if any
		desc, err := r.CfnClient.DescribeStack(clientStack)

		if err != nil {
			var e *cloudformation.ErrStackNotFound
			if errors.As(err, &e) {
				// The stack is successfully deleted, no re-queue needed
				// Remove our finalizer from the list and update it.
				controllerutil.RemoveFinalizer(&cfnStack, cfnv1.CloudFormationStackFinalizer)
				err := r.Update(ctx, &cfnStack)
				if err != nil {
					log.Error(err, fmt.Sprintf("Failed to remove finalizer from stack object '%s/%s'", cfnStack.Namespace, cfnStack.Name))
				}
				log.Info(fmt.Sprintf("Successfully deleted stack '%s'", clientStack.Name))
				return cfnv1.CloudFormationStackReady(cfnStack, ""), ctrl.Result{}, err
			} else {
				msg := fmt.Sprintf("Failed to describe the stack '%s'", clientStack.Name)
				log.Error(err, msg)
				cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{Message: msg, Reason: cfnv1.CloudFormationApiCallFailedReason})
				return cfnStack, ctrl.Result{RequeueAfter: cfnStack.GetRetryInterval()}, err
			}
		}

		if desc.InProgress() {
			// Let the current action complete before deleting the stack
			msg := fmt.Sprintf("Stack action is in progress for stack marked for deletion '%s' (status '%s'), waiting for stack action to complete", clientStack.Name, desc.StackStatus)
			log.Info(msg)
			cfnStack = cfnv1.CloudFormationStackProgressing(cfnStack, cfnv1.ReadinessUpdate{Message: msg})
			return cfnStack, ctrl.Result{RequeueAfter: cfnStack.Spec.PollInterval.Duration}, err
		}

		if desc.ReadyForCleanup() {
			// start the stack deletion
			if err := r.CfnClient.DeleteStack(clientStack); err != nil {
				msg := fmt.Sprintf("Failed to delete the stack '%s'", clientStack.Name)
				log.Error(err, msg)
				cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{Message: msg, Reason: cfnv1.CloudFormationApiCallFailedReason})
				return cfnStack, ctrl.Result{RequeueAfter: cfnStack.GetRetryInterval()}, err
			}
			// TODO emit error event if we entered delete failed state
			msg := fmt.Sprintf("Started deletion of stack '%s'", clientStack.Name)
			log.Info(msg)
			cfnStack = cfnv1.CloudFormationStackProgressing(cfnStack, cfnv1.ReadinessUpdate{Message: msg})
			return cfnStack, ctrl.Result{RequeueAfter: cfnStack.Spec.PollInterval.Duration}, nil
		}

		msg := fmt.Sprintf("Unexpected stack status for stack '%s': %s", clientStack.Name, desc.StackStatus)
		if desc.StackStatusReason != nil {
			msg = fmt.Sprintf("%s (reason '%s')", msg, *desc.StackStatusReason)
		}
		log.Error(err, msg)
		cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, cfnv1.ReadinessUpdate{Message: msg, Reason: cfnv1.UnexpectedStatusReason})
		return cfnStack, ctrl.Result{RequeueAfter: cfnStack.GetRetryInterval()}, err
	}

	ctrl.LoggerFrom(ctx).Info("Skipping CloudFormation stack deletion for suspended resource")
	// Remove finalizer
	controllerutil.RemoveFinalizer(&cfnStack, cfnv1.CloudFormationStackFinalizer)
	err := r.Update(ctx, &cfnStack)
	return cfnStack, ctrl.Result{}, err
}
