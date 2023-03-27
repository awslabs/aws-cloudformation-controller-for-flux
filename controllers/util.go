package controllers

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	cfnv1 "github.com/awslabs/aws-cloudformation-controller-for-flux/api/v1alpha1"
	securejoin "github.com/cyphar/filepath-securejoin"
	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/dependency"
	"github.com/fluxcd/pkg/untar"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/hashicorp/go-retryablehttp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/reference"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// These methods were originally from
// https://github.com/weaveworks/tf-controller/blob/main/controllers/tf_controller.go

/**
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
**/

func (r *CloudFormationStackReconciler) requestsForRevisionChangeOf(indexKey string) func(obj client.Object) []reconcile.Request {
	return func(obj client.Object) []reconcile.Request {
		repo, ok := obj.(interface {
			GetArtifact() *sourcev1.Artifact
		})
		if !ok {
			panic(fmt.Sprintf("Expected an object conformed with GetArtifact() method, but got a %T", obj))
		}
		// If we do not have an artifact, we have no requests to make
		if repo.GetArtifact() == nil {
			return nil
		}

		ctx := context.Background()
		var list cfnv1.CloudFormationStackList
		if err := r.List(ctx, &list, client.MatchingFields{
			indexKey: client.ObjectKeyFromObject(obj).String(),
		}); err != nil {
			return nil
		}
		var dd []dependency.Dependent
		for _, d := range list.Items {
			// If the revision of the artifact equals to the last attempted revision,
			// we should not make a request for this CloudFormation stack
			if repo.GetArtifact().HasRevision(d.Status.LastAttemptedRevision) {
				continue
			}
			dd = append(dd, d.DeepCopy())
		}
		sorted, err := dependency.Sort(dd)
		if err != nil {
			return nil
		}
		reqs := make([]reconcile.Request, len(sorted))
		for i, t := range sorted {
			reqs[i].NamespacedName.Name = t.Name
			reqs[i].NamespacedName.Namespace = t.Namespace
		}
		return reqs
	}
}

func (r *CloudFormationStackReconciler) patchStatus(ctx context.Context, cfnStack *cfnv1.CloudFormationStack) error {
	key := client.ObjectKeyFromObject(cfnStack)
	latest := &cfnv1.CloudFormationStack{}
	if err := r.Client.Get(ctx, key, latest); err != nil {
		return err
	}
	return r.Client.Status().Patch(ctx, cfnStack, client.MergeFrom(latest))
}

func (r *CloudFormationStackReconciler) recordSuspension(ctx context.Context, cfnStack cfnv1.CloudFormationStack) {
	if r.MetricsRecorder == nil {
		return
	}
	log := ctrl.LoggerFrom(ctx)

	objRef, err := reference.GetReference(r.Scheme, &cfnStack)
	if err != nil {
		log.Error(err, "Unable to record suspended metric")
		return
	}

	if !cfnStack.DeletionTimestamp.IsZero() {
		r.MetricsRecorder.RecordSuspend(*objRef, false)
	} else {
		r.MetricsRecorder.RecordSuspend(*objRef, cfnStack.Spec.Suspend)
	}
}

func (r *CloudFormationStackReconciler) recordReadiness(ctx context.Context, cfnStack cfnv1.CloudFormationStack) {
	if r.MetricsRecorder == nil {
		return
	}

	objRef, err := reference.GetReference(r.Scheme, &cfnStack)
	if err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "Unable to record readiness metric")
		return
	}
	if rc := apimeta.FindStatusCondition(cfnStack.Status.Conditions, meta.ReadyCondition); rc != nil {
		r.MetricsRecorder.RecordCondition(*objRef, *rc, !cfnStack.DeletionTimestamp.IsZero())
	} else {
		r.MetricsRecorder.RecordCondition(*objRef, metav1.Condition{
			Type:   meta.ReadyCondition,
			Status: metav1.ConditionUnknown,
		}, !cfnStack.DeletionTimestamp.IsZero())
	}
}

// getSource retrieves the Source object for the CloudFormation stack template
func (r *CloudFormationStackReconciler) getSource(ctx context.Context, cfnStack cfnv1.CloudFormationStack) (sourcev1.Source, error) {
	var sourceObj sourcev1.Source

	sourceNamespace := cfnStack.GetNamespace()
	if cfnStack.Spec.SourceRef.Namespace != "" {
		sourceNamespace = cfnStack.Spec.SourceRef.Namespace
	}
	namespacedName := types.NamespacedName{
		Namespace: sourceNamespace,
		Name:      cfnStack.Spec.SourceRef.Name,
	}

	switch cfnStack.Spec.SourceRef.Kind {
	case sourcev1.GitRepositoryKind:
		var repository sourcev1.GitRepository
		err := r.Client.Get(ctx, namespacedName, &repository)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return sourceObj, err
			}
			return sourceObj, fmt.Errorf("unable to get source '%s': %w", namespacedName, err)
		}
		sourceObj = &repository
	case sourcev1.BucketKind:
		var bucket sourcev1.Bucket
		err := r.Client.Get(ctx, namespacedName, &bucket)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return sourceObj, err
			}
			return sourceObj, fmt.Errorf("unable to get source '%s': %w", namespacedName, err)
		}
		sourceObj = &bucket
	case sourcev1.OCIRepositoryKind:
		var repository sourcev1.OCIRepository
		err := r.Client.Get(ctx, namespacedName, &repository)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return sourceObj, err
			}
			return sourceObj, fmt.Errorf("unable to get source '%s': %w", namespacedName, err)
		}
		sourceObj = &repository
	default:
		return sourceObj, fmt.Errorf("source `%s` kind '%s' not supported",
			cfnStack.Spec.SourceRef.Name, cfnStack.Spec.SourceRef.Kind)
	}
	return sourceObj, nil
}

// These methods were originally from
// https://github.com/fluxcd/helm-controller/blob/main/controllers/helmrelease_controller_chart.go

/*
Copyright 2020 The Flux authors

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

// loadCloudFormationTemplate attempts to download the artifact from the provided source,
// loads the CloudFormation template file into memory, then removes the downloaded artifact.
// It returns the loaded template on success, or returns an error.
func (r *CloudFormationStackReconciler) loadCloudFormationTemplate(ctx context.Context, cfnStack cfnv1.CloudFormationStack, artifact *sourcev1.Artifact) (*bytes.Buffer, error) {
	log := ctrl.LoggerFrom(ctx)

	// download the artifact targz file
	artifactURL := artifact.URL
	if hostname := os.Getenv("SOURCE_CONTROLLER_LOCALHOST"); hostname != "" {
		u, err := url.Parse(artifactURL)
		if err != nil {
			return nil, err
		}
		u.Host = hostname
		artifactURL = u.String()
	}

	req, err := retryablehttp.NewRequest(http.MethodGet, artifactURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new request: %w", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download artifact, error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download artifact, status code: %s", resp.Status)
	}

	// verify checksum matches origin
	var buf bytes.Buffer
	if err := r.copyAndVerifyArtifact(artifact, &buf, resp.Body); err != nil {
		return nil, err
	}

	// extract artifact into temp dir
	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("%s-%s", cfnStack.GetNamespace(), cfnStack.GetName()))
	if err != nil {
		msg := fmt.Sprintf("unable to create temp dir for namespace %s, name %s", cfnStack.GetNamespace(), cfnStack.GetName())
		log.Error(err, msg)
		return nil, fmt.Errorf(msg)
	}
	defer os.RemoveAll(tmpDir)

	if _, err = untar.Untar(&buf, tmpDir); err != nil {
		msg := fmt.Sprintf("failed to untar artifact, namespace %s, name %s", cfnStack.GetNamespace(), cfnStack.GetName())
		log.Error(err, msg)
		return nil, fmt.Errorf(msg)
	}

	// load the template file
	templateFilePath, err := securejoin.SecureJoin(tmpDir, cfnStack.Spec.TemplatePath)
	if err != nil {
		msg := fmt.Sprintf("unable to join securely the artifact temp directory with template path '%s'", cfnStack.Spec.TemplatePath)
		log.Error(err, msg)
		return nil, fmt.Errorf(msg)
	}

	templateBytes, err := os.ReadFile(templateFilePath)
	if err != nil {
		msg := fmt.Sprintf("unable to read template file '%s' in the artifact temp directory", cfnStack.Spec.TemplatePath)
		log.Error(err, msg)
		return nil, fmt.Errorf(msg)
	}

	return bytes.NewBuffer(templateBytes), nil
}

func (r *CloudFormationStackReconciler) copyAndVerifyArtifact(artifact *sourcev1.Artifact, buf *bytes.Buffer, reader io.Reader) error {
	hasher := sha256.New()

	// for backwards compatibility with source-controller v0.17.2 and older
	if len(artifact.Checksum) == 40 {
		hasher = sha1.New()
	}

	// compute checksum
	mw := io.MultiWriter(hasher, buf)
	if _, err := io.Copy(mw, reader); err != nil {
		return err
	}

	if checksum := fmt.Sprintf("%x", hasher.Sum(nil)); checksum != artifact.Checksum {
		return fmt.Errorf("failed to verify artifact: computed checksum '%s' doesn't match advertised '%s'",
			checksum, artifact.Checksum)
	}

	return nil
}

// event emits a Kubernetes event and forwards the event to notification controller if configured.
func (r *CloudFormationStackReconciler) event(_ context.Context, cfnStack cfnv1.CloudFormationStack, revision, severity, msg string) {
	var meta map[string]string
	if revision != "" {
		meta = map[string]string{cfnv1.GroupVersion.Group + "/revision": revision}
	}
	eventtype := "Normal"
	if severity == eventv1.EventSeverityError {
		eventtype = "Warning"
	}
	r.EventRecorder.AnnotatedEventf(&cfnStack, meta, eventtype, severity, msg)
}
