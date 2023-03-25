// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package controllers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	cfnv1 "github.com/awslabs/aws-cloudformation-controller-for-flux/api/v1alpha1"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/cloudformation"
	clientmocks "github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/mocks"
	clienttypes "github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/types"
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
	mockStackName                = "mock-stack"
	mockNamespace                = "mock-namespace"
	mockGenerationId             = 2
	mockSourceNamespace          = "mock-source-namespace"
	mockRealStackName            = "mock-real-stack"
	mockTemplatePath             = "template.yaml"
	mockTemplateGitRepoName      = "mock-cfn-template-git-repo"
	mockTemplateOCIRepoName      = "mock-cfn-template-oci-repo"
	mockTemplateSourceBucketName = "mock-cfn-template-source-bucket"
	mockSourceRevision           = "main@sha1:132f4e719209eb10b9485302f8593fc0e680f4fc"
	mockTemplateSourceFile       = "../examples/my-cloudformation-templates/template.yaml"
	mockTemplateUploadBucket     = "mock-template-upload-bucket"
	mockChangeSetName            = "flux-2-main-sha1-132f4e719209eb10b9485302f8593fc0e680f4fc"
	mockChangeSetArn             = "arn:aws:cloudformation:us-west-2:111:changeSet/flux-2-main-sha1-132f4e719209eb10b9485302f8593fc0e680f4fc/9edc39b0-ee18-440d-823e-3dda74646b2"
	mockTemplateS3Url            = "https://mock-template-upload-bucket.s3.mock-region.amazonaws.com/mock-flux-template-file-object-key"
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

	mockBucketSourceRef = cfnv1.SourceReference{
		Kind:      sourcev1.BucketKind,
		Name:      mockTemplateSourceBucketName,
		Namespace: mockSourceNamespace,
	}
	mockBucketSourceReference = types.NamespacedName{
		Name:      mockTemplateSourceBucketName,
		Namespace: mockSourceNamespace,
	}

	mockIntervalDuration, _      = time.ParseDuration("5h")
	mockRetryIntervalDuration, _ = time.ParseDuration("2m")
	mockPollIntervalDuration, _  = time.ParseDuration("30s")
	mockInterval                 = metav1.Duration{Duration: mockIntervalDuration}
	mockRetryInterval            = metav1.Duration{Duration: mockRetryIntervalDuration}
	mockPollInterval             = metav1.Duration{Duration: mockPollIntervalDuration}

	mockTemplateSourceFileContents string
	mockTestArtifactBytes          []byte
	mockTemplateContentsChecksum   string
)

func init() {
	utilruntime.Must(createTestArtifact())
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))
	utilruntime.Must(cfnv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func createTestArtifact() error {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	file, err := os.Open(mockTemplateSourceFile)
	if err != nil {
		return err
	}

	info, err := file.Stat()
	if err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, info.Name())
	if err != nil {
		return err
	}
	header.Name = mockTemplatePath
	if err = tw.WriteHeader(header); err != nil {
		return err
	}

	if _, err = io.Copy(tw, file); err != nil {
		return err
	}

	if err = file.Close(); err != nil {
		return err
	}

	if err = tw.Close(); err != nil {
		return err
	}

	if err = gw.Close(); err != nil {
		return err
	}

	mockTestArtifactBytes = buf.Bytes()

	h := sha256.New()
	if _, err = h.Write(mockTestArtifactBytes); err != nil {
		return err
	}
	mockTemplateContentsChecksum = fmt.Sprintf("%x", h.Sum(nil))

	templateBytes, err := os.ReadFile(mockTemplateSourceFile)
	if err != nil {
		return err
	}
	mockTemplateSourceFileContents = string(templateBytes)

	return nil
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
			w.Write(mockTestArtifactBytes)
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
			cfnStack.Generation = mockGenerationId
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

		// Describe the real CFN stack
		expectedDescribeStackIn := &clienttypes.Stack{
			Name:           mockRealStackName,
			Generation:     mockGenerationId,
			SourceRevision: mockSourceRevision,
			StackConfig: &clienttypes.StackConfig{
				TemplateBucket: mockTemplateUploadBucket,
				TemplateBody:   mockTemplateSourceFileContents,
			},
		}
		cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(nil, &cloudformation.ErrStackNotFound{})

		expectedDescribeChangeSetIn := &clienttypes.Stack{
			Name:           mockRealStackName,
			Generation:     mockGenerationId,
			SourceRevision: mockSourceRevision,
			StackConfig: &clienttypes.StackConfig{
				TemplateBucket: mockTemplateUploadBucket,
				TemplateBody:   mockTemplateSourceFileContents,
			},
		}
		cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetNotFound{})

		expectedCreateStackIn := &clienttypes.Stack{
			Name:           mockRealStackName,
			Generation:     mockGenerationId,
			SourceRevision: mockSourceRevision,
			StackConfig: &clienttypes.StackConfig{
				TemplateBucket: mockTemplateUploadBucket,
				TemplateBody:   mockTemplateSourceFileContents,
				TemplateURL:    mockTemplateS3Url,
			},
		}
		cfnClient.EXPECT().CreateStack(expectedCreateStackIn).Return(mockChangeSetArn, nil)

		s3Client.EXPECT().UploadTemplate(mockTemplateUploadBucket, "", gomock.Any(), strings.NewReader(mockTemplateSourceFileContents)).Return(mockTemplateS3Url, nil)

		k8sClient.EXPECT().Status().Return(k8sStatusWriter).AnyTimes()

		eventRecorder.EXPECT().AnnotatedEventf(
			gomock.Any(),
			gomock.Any(),
			"Normal",
			"info",
			"Creation of stack 'mock-real-stack' in progress (change set arn:aws:cloudformation:us-west-2:111:changeSet/flux-2-main-sha1-132f4e719209eb10b9485302f8593fc0e680f4fc/9edc39b0-ee18-440d-823e-3dda74646b2)",
		)

		// TODO fill in the patching calls we expect the controller to make
		k8sClient.EXPECT().Patch(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
		).Return(nil).AnyTimes()
		k8sStatusWriter.EXPECT().Patch(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
		).Return(nil).AnyTimes()

		reconciler := &CloudFormationStackReconciler{
			Scheme:          scheme,
			Client:          k8sClient,
			CfnClient:       cfnClient,
			S3Client:        s3Client,
			TemplateBucket:  mockTemplateUploadBucket,
			EventRecorder:   eventRecorder,
			MetricsRecorder: metricsRecorder,
			httpClient:      httpClient,
		}

		request := ctrl.Request{NamespacedName: mockStackNamespacedName}

		// WHEN
		result, err := reconciler.Reconcile(ctx, request)

		// THEN
		require.NoError(t, err)
		require.False(t, result.Requeue)
		require.Equal(t, mockPollIntervalDuration, result.RequeueAfter)
	})
}
