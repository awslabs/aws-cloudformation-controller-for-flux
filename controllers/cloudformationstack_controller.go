// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package controllers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/metrics"
	"github.com/fluxcd/pkg/runtime/predicates"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"

	cfnv1 "github.com/awslabs/aws-cloudformation-controller-for-flux/api/v1alpha1"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/cloudformation"
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
	/*if !controllerutil.ContainsFinalizer(&cfnStack, cfnv1.CloudFormationStackFinalizer) {
		patch := client.MergeFrom(cfnStack.DeepCopy())
		controllerutil.AddFinalizer(&cfnStack, cfnv1.CloudFormationStackFinalizer)
		if err := r.Patch(ctx, &cfnStack, patch); err != nil {
			log.Error(err, "unable to register finalizer")
			return ctrl.Result{}, err
		}
	}
	*/

	// Check if the CloudFormationStack is suspended
	if cfnStack.Spec.Suspend {
		log.Info("Reconciliation is suspended for this object")
		return ctrl.Result{}, nil
	}

	// Check if the object is being deleted
	if !cfnStack.ObjectMeta.DeletionTimestamp.IsZero() {
		cfnStack, result, err := r.reconcileDelete(ctx, cfnStack)

		// Update status
		if updateStatusErr := r.patchStatus(ctx, &cfnStack); updateStatusErr != nil {
			log.Error(updateStatusErr, "unable to update status after reconciliation")
			return ctrl.Result{Requeue: true}, updateStatusErr
		}

		return result, err
	}

	// Reconcile
	cfnStack, result, err := r.reconcile(ctx, cfnStack)

	// Update status
	if updateStatusErr := r.patchStatus(ctx, &cfnStack); updateStatusErr != nil {
		log.Error(updateStatusErr, "unable to update status after reconciliation")
		return ctrl.Result{Requeue: true}, updateStatusErr
	}

	// Record ready status
	r.recordReadiness(ctx, cfnStack)

	// Log reconciliation duration
	durationMsg := fmt.Sprintf("reconcilation finished in %s", time.Now().Sub(start).String())
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
		cfnStack.Status.ObservedGeneration = cfnStack.Generation
		cfnStack = cfnv1.CloudFormationStackProgressing(cfnStack, "Stack reconciliation in progress")
		if updateStatusErr := r.patchStatus(ctx, &cfnStack); updateStatusErr != nil {
			log.Error(updateStatusErr, "unable to update status after generation update")
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
			cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, "", cfnv1.ArtifactFailedReason, msg)
			log.Info(msg)
			// do not requeue immediately, when the source is created the watcher should trigger a reconciliation
			return cfnStack, ctrl.Result{RequeueAfter: cfnStack.GetRetryInterval()}, nil
		} else {
			// retry on transient errors
			msg := fmt.Sprintf("Failed to resolve source '%s'", cfnStack.Spec.SourceRef.String())
			log.Error(err, msg)
			cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, "", cfnv1.ArtifactFailedReason, msg)
			return cfnStack, ctrl.Result{Requeue: true}, err
		}
	}

	// Check source readiness
	if sourceObj.GetArtifact() == nil {
		msg := fmt.Sprintf("Source '%s' is not ready, artifact not found", cfnStack.Spec.SourceRef.String())
		cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, "", cfnv1.ArtifactFailedReason, msg)
		log.Info(msg)
		// do not requeue immediately, when the artifact is created the watcher should trigger a reconciliation
		return cfnStack, ctrl.Result{RequeueAfter: cfnStack.GetRetryInterval()}, nil
	}

	// Load stack template file from artifact
	templateContents, err := r.loadCloudFormationTemplate(cfnStack, sourceObj.GetArtifact())
	if err != nil {
		msg := fmt.Sprintf("Failed to load template '%s' from source '%s'", cfnStack.Spec.TemplatePath, cfnStack.Spec.SourceRef.String())
		log.Error(err, msg)
		cfnStack = cfnv1.CloudFormationStackNotReady(cfnStack, "", cfnv1.ArtifactFailedReason, msg)
		return cfnStack, ctrl.Result{RequeueAfter: cfnStack.GetRetryInterval()}, nil
	}

	// Check dependencies
	/*if len(cfnStack.Spec.DependsOn) > 0 {
		if err := r.checkDependencies(cfnStack); err != nil {
			msg := fmt.Sprintf("dependencies do not meet ready condition (%s), retrying in %s",
				err.Error(), r.requeueDependency.String())
			r.event(ctx, cfnStack, hc.GetArtifact().Revision, eventv1.EventSeverityInfo, msg)
			log.Info(msg)

			// Exponential backoff would cause execution to be prolonged too much,
			// instead we requeue on a fixed interval.
			return cfnv1.HelmReleaseNotReady(cfnStack,
				cfnv1.DependencyNotReadyReason, err.Error()), ctrl.Result{RequeueAfter: r.requeueDependency}, nil
		}
		log.Info("all dependencies are ready, proceeding with release")
	}
	*/

	// Compose values
	/*values, err := r.composeValues(ctx, cfnStack)
	if err != nil {
		r.event(ctx, cfnStack, cfnStack.Status.LastAttemptedRevision, eventv1.EventSeverityError, err.Error())
		return cfnv1.HelmReleaseNotReady(cfnStack, cfnv1.InitFailedReason, err.Error()), ctrl.Result{Requeue: true}, nil
	}
	*/

	// Reconcile CloudFormation stack
	reconciledCfnStack, requeueInterval, err := r.reconcileStack(ctx, *cfnStack.DeepCopy(), templateContents)
	if err != nil {
		log.Error(err, "Failed to reconcile stack")
		return reconciledCfnStack, ctrl.Result{RequeueAfter: requeueInterval}, err
	}

	return reconciledCfnStack, ctrl.Result{RequeueAfter: requeueInterval}, nil
}

func (r *CloudFormationStackReconciler) reconcileStack(ctx context.Context, cfnStack cfnv1.CloudFormationStack, templateContents *bytes.Buffer) (cfnv1.CloudFormationStack, time.Duration, error) {
	log := ctrl.LoggerFrom(ctx)

	clientStack := toClientStack(cfnStack)
	clientStack.TemplateBody = templateContents.String()

	desc, err := r.CfnClient.DescribeStack(clientStack)

	// TODO need to update stack conditions everywhere before returning

	// Check if the stack exists; if not, create it
	if err != nil {
		var e *cloudformation.ErrStackNotFound
		if errors.As(err, &e) {
			return r.reconcileChangeset(ctx, cfnStack, clientStack, true)
		} else {
			log.Error(err, "Failed to describe the stack")
			return cfnStack, cfnStack.GetRetryInterval(), err
		}
	}

	// Keep polling if the stack is still in progress
	if desc.InProgress() {
		return cfnStack, cfnStack.Spec.PollInterval.Duration, nil
	}

	// Continue rollback if a previous update rollback failed
	if desc.RequiresRollbackContinuation() {
		if err := r.CfnClient.ContinueRollback(clientStack); err != nil {
			log.Error(err, "Failed to continue rollback")
			return cfnStack, cfnStack.GetRetryInterval(), err
		}
		return cfnStack, cfnStack.Spec.PollInterval.Duration, nil
	}

	// Delete the stack if it has failed to create or delete
	if desc.RequiresCleanup() {
		if err := r.CfnClient.DeleteStack(clientStack); err != nil {
			log.Error(err, "Failed to delete stack")
			return cfnStack, cfnStack.GetRetryInterval(), err
		}
		return cfnStack, cfnStack.Spec.PollInterval.Duration, nil
	}

	// Check if the stack is ready for an update
	if desc.IsSuccess() || desc.IsRecoverableFailure() {
		return r.reconcileChangeset(ctx, cfnStack, clientStack, false)
	}

	// Do nothing
	// TODO should we be able to get here??
	return cfnv1.CloudFormationStackReady(cfnStack), cfnStack.Spec.Interval.Duration, nil
}

func (r *CloudFormationStackReconciler) reconcileChangeset(ctx context.Context, cfnStack cfnv1.CloudFormationStack, clientStack *cloudformation.Stack, isCreate bool) (cfnv1.CloudFormationStack, time.Duration, error) {
	// TODO need to update stack conditions everywhere before returning
	log := ctrl.LoggerFrom(ctx)

	desc, err := r.CfnClient.DescribeChangeSet(clientStack)

	// Check if the change set exists; if not, create it.
	// If the change set is empty, we can delete it and declare success
	if err != nil {
		var notFoundErr *cloudformation.ErrChangeSetNotFound
		var emptyErr *cloudformation.ErrChangeSetEmpty
		if errors.As(err, &notFoundErr) {
			_, err := r.CfnClient.CreateStack(clientStack)
			if err != nil {
				log.Error(err, "Failed to create change set to create the stack")
				return cfnStack, cfnStack.GetRetryInterval(), err
			}
			// TODO figure out how to save the new change set ARN to the stack object
			return cfnStack, cfnStack.Spec.PollInterval.Duration, nil
		} else if errors.As(err, &emptyErr) {
			// This changeset was empty, meaning that the stack is up to date with the latest template
			if err := r.CfnClient.DeleteChangeSet(clientStack); err != nil {
				log.Error(err, "Failed to delete empty change set")
				return cfnStack, cfnStack.GetRetryInterval(), err
			}
			return cfnv1.CloudFormationStackReady(cfnStack), cfnStack.Spec.Interval.Duration, nil
		} else {
			log.Error(err, "Failed to describe the change set")
			return cfnStack, cfnStack.GetRetryInterval(), err
		}
	}

	// If change set failed, delete it so we can create it again
	if desc.IsFailed() {
		if err := r.CfnClient.DeleteChangeSet(clientStack); err != nil {
			log.Error(err, "Failed to delete failed change set")
			return cfnStack, cfnStack.GetRetryInterval(), err
		}
		return cfnStack, cfnStack.GetRetryInterval(), nil
	}

	// Keep polling if the change set is still in progress
	if desc.InProgress() {
		return cfnStack, cfnStack.Spec.PollInterval.Duration, nil
	}

	// This changeset was successfully applied, meaning that the stack is up to date with the latest template
	if desc.IsSuccess() {
		return cfnv1.CloudFormationStackReady(cfnStack), cfnStack.Spec.Interval.Duration, nil
	}

	// Start the change set execution
	if desc.ReadyForExecution() {
		if err := r.CfnClient.ExecuteChangeSet(clientStack); err != nil {
			log.Error(err, "Failed to execute change set")
			return cfnStack, cfnStack.GetRetryInterval(), err
		}
		return cfnStack, cfnStack.Spec.PollInterval.Duration, nil
	}

	// TODO should we be able to get here??
	return cfnv1.CloudFormationStackReady(cfnStack), cfnStack.Spec.Interval.Duration, nil
}

// reconcileDelete deletes the CloudFormation stack.
func (r *CloudFormationStackReconciler) reconcileDelete(ctx context.Context, cfnStack cfnv1.CloudFormationStack) (cfnv1.CloudFormationStack, ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	r.recordReadiness(ctx, cfnStack)

	if !cfnStack.Spec.Suspend {
		clientStack := toClientStack(cfnStack)
		desc, err := r.CfnClient.DescribeStack(clientStack)

		if err != nil {
			var e *cloudformation.ErrStackNotFound
			if errors.As(err, &e) {
				// The stack is successfully deleted, no re-queue needed
				// TODO figure out the right status for successful deletion
				return cfnv1.CloudFormationStackReady(cfnStack), ctrl.Result{}, nil
			} else {
				log.Error(err, "Failed to describe the stack")
				return cfnStack, ctrl.Result{RequeueAfter: cfnStack.GetRetryInterval()}, err
			}
		}

		if desc.InProgress() {
			// Let the current action complete before deleting the stack
			return cfnStack, ctrl.Result{RequeueAfter: cfnStack.Spec.PollInterval.Duration}, err
		}

		if desc.ReadyForCleanup() {
			// start the stack deletion
			if err := r.CfnClient.DeleteStack(clientStack); err != nil {
				log.Error(err, "Failed to delete stack")
				return cfnStack, ctrl.Result{RequeueAfter: cfnStack.GetRetryInterval()}, err
			}
			return cfnStack, ctrl.Result{RequeueAfter: cfnStack.Spec.PollInterval.Duration}, nil
		}

		// TODO should we be able to get here??
	} else {
		ctrl.LoggerFrom(ctx).Info("skipping CloudFormation stack deletion for suspended resource")
	}

	// Remove our finalizer from the list and update it.
	/*controllerutil.RemoveFinalizer(&hr, cfnv1.CloudFormationStackFinalizer)
	if err := r.Update(ctx, &hr); err != nil {
		return ctrl.Result{}, err
	}
	*/

	return cfnStack, ctrl.Result{}, nil
}

// Converts the Flux controller stack type into the CloudFormation client stack type
func toClientStack(cfnStack cfnv1.CloudFormationStack) *cloudformation.Stack {
	// TODO figure out how to fill in the change set ARN

	return &cloudformation.Stack{
		Name:       cfnStack.Spec.StackName,
		Region:     cfnStack.Spec.Region,
		Generation: cfnStack.Generation,
	}
}
