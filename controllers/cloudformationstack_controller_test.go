// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package controllers

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cfnv1 "github.com/awslabs/aws-cloudformation-controller-for-flux/api/v1alpha1"
	clientmocks "github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/mocks"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/mocks"
	"github.com/fluxcd/pkg/runtime/metrics"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/golang/mock/gomock"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

const (
	mockStackName           = "mock-stack"
	mockNamespace           = "mock-namespace"
	mockSourceNamespace     = "mock-source-namespace"
	mockRealStackName       = "mock-real-stack"
	mockTemplatePath        = "template.yaml"
	mockTemplateGitRepoName = "mock-cfn-template-git-repo"
	mockTemplateOCIRepoName = "mock-cfn-template-oci-repo"
	mockTemplateBucketName  = "mock-cfn-template-bucket"
	mockSourceRevision      = "main@sha1:132f4e719209eb10b9485302f8593fc0e680f4fc"
)

var (
	scheme = runtime.NewScheme()

	mockStackNamespacedName = types.NamespacedName{
		Name:      mockStackName,
		Namespace: mockNamespace,
	}

	mockGitRef = cfnv1.SourceReference{
		Kind:      sourcev1.GitRepositoryKind,
		Name:      mockTemplateGitRepoName,
		Namespace: mockSourceNamespace,
	}
	mockGitSourceReference = types.NamespacedName{
		Name:      mockTemplateGitRepoName,
		Namespace: mockSourceNamespace,
	}

	mockOCIRef = cfnv1.SourceReference{
		Kind:      sourcev1.OCIRepositoryKind,
		Name:      mockTemplateOCIRepoName,
		Namespace: mockSourceNamespace,
	}
	mockOCISourceReference = types.NamespacedName{
		Name:      mockTemplateOCIRepoName,
		Namespace: mockSourceNamespace,
	}

	mockBucketRef = cfnv1.SourceReference{
		Kind:      sourcev1.BucketKind,
		Name:      mockTemplateBucketName,
		Namespace: mockSourceNamespace,
	}
	mockBucketReference = types.NamespacedName{
		Name:      mockTemplateBucketName,
		Namespace: mockSourceNamespace,
	}

	mockIntervalDuration, _      = time.ParseDuration("5h")
	mockRetryIntervalDuration, _ = time.ParseDuration("2m")
	mockPollIntervalDuration, _  = time.ParseDuration("30s")
	mockInterval                 = metav1.Duration{Duration: mockIntervalDuration}
	mockRetryInterval            = metav1.Duration{Duration: mockRetryIntervalDuration}
	mockPollInterval             = metav1.Duration{Duration: mockPollIntervalDuration}

	mockTemplateContents         = []byte("hello world")
	mockTemplateContentsChecksum = fmt.Sprintf("%x", sha256.New().Sum(mockTemplateContents))
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))
	utilruntime.Must(cfnv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func TestCfnController_Draft(t *testing.T) {
	t.Run("first draft", func(t *testing.T) {
		// GIVEN
		mockCtrl, ctx := gomock.WithContext(context.Background(), t)

		cfnClient := clientmocks.NewMockCloudFormationClient(mockCtrl)
		s3Client := clientmocks.NewMockS3Client(mockCtrl)
		k8sClient := mocks.NewMockClient(mockCtrl)
		k8sStatusWriter := mocks.NewMockStatusWriter(mockCtrl)
		eventRecorder := mocks.NewMockEventRecorder(mockCtrl)
		metricsRecorder := metrics.NewRecorder()

		httpClient := retryablehttp.NewClient()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/path.tar.gz" {
				t.Errorf("Expected to request '/path.tar.gz', got: %s", r.URL.Path)
			}
			if r.Method != "GET" {
				t.Errorf("Expected to do a GET request, got: %s", r.Method)
			}
			w.WriteHeader(http.StatusOK)
			w.Write(mockTemplateContents)
		}))
		defer server.Close()
		mockSourceArtifactURL := server.URL + "/path.tar.gz"

		// Get the initial CFNStack object that the controller will work off of
		k8sClient.EXPECT().Get(
			gomock.Any(),
			mockStackNamespacedName,
			gomock.Any(),
		).DoAndReturn(func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
			cfnStack, ok := obj.(*cfnv1.CloudFormationStack)
			if !ok {
				return errors.New(fmt.Sprintf("Expected a CloudFormationStack object, but got a %T", obj))
			}
			cfnStack.Name = mockStackName
			cfnStack.Namespace = mockNamespace
			cfnStack.Spec = cfnv1.CloudFormationStackSpec{
				StackName:              mockRealStackName,
				TemplatePath:           mockTemplatePath,
				SourceRef:              mockGitRef,
				Interval:               mockInterval,
				RetryInterval:          &mockRetryInterval,
				PollInterval:           mockPollInterval,
				Suspend:                false,
				DestroyStackOnDeletion: false,
			}
			return nil
		}).AnyTimes()

		// Get the source reference
		k8sClient.EXPECT().Get(
			gomock.Any(),
			mockGitSourceReference,
			gomock.Any(),
		).DoAndReturn(func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
			gitRepo, ok := obj.(*sourcev1.GitRepository)
			if !ok {
				return errors.New(fmt.Sprintf("Expected a GitRepository object, but got a %T", obj))
			}
			gitRepo.Name = mockTemplateGitRepoName
			gitRepo.Namespace = mockSourceNamespace
			gitRepo.Status = sourcev1.GitRepositoryStatus{
				Artifact: &sourcev1.Artifact{
					URL:      mockSourceArtifactURL,
					Revision: mockSourceRevision,
					Checksum: mockTemplateContentsChecksum,
				},
			}
			return nil
		}).AnyTimes()

		k8sClient.EXPECT().Status().Return(k8sStatusWriter)

		// TODO fill in the patching calls we expect the controller to make
		k8sClient.EXPECT().Patch(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
		).Return(nil)
		k8sStatusWriter.EXPECT().Patch(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
		).Return(nil)

		reconciler := &CloudFormationStackReconciler{
			Scheme:          scheme,
			Client:          k8sClient,
			CfnClient:       cfnClient,
			S3Client:        s3Client,
			EventRecorder:   eventRecorder,
			MetricsRecorder: metricsRecorder,
			httpClient:      httpClient,
		}

		request := ctrl.Request{NamespacedName: mockStackNamespacedName}

		// WHEN
		result, err := reconciler.Reconcile(ctx, request)

		// THEN
		require.NoError(t, err)
		require.True(t, result.Requeue)
		expectedRequeueDelay, _ := time.ParseDuration("5s")
		require.Equal(t, expectedRequeueDelay, result.RequeueAfter)
	})
}
