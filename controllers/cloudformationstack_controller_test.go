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
	mockChangeSetArn             = "arn:aws:cloudformation:us-west-2:111:changeSet/mock-change-set"
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

// Compare field by field instead of using Equals, to avoid comparing timestamps
func compareCfnStackStatus(t *testing.T, expectedStackStatus *cfnv1.CloudFormationStackStatus, actualStackStatus *cfnv1.CloudFormationStackStatus) {
	require.Equalf(t, expectedStackStatus.ObservedGeneration, actualStackStatus.ObservedGeneration, "ObservedGeneration in stack status not equal")
	require.Equalf(t, expectedStackStatus.LastAppliedRevision, actualStackStatus.LastAppliedRevision, "LastAppliedRevision in stack status not equal")
	require.Equalf(t, expectedStackStatus.LastAttemptedRevision, actualStackStatus.LastAttemptedRevision, "LastAttemptedRevision in stack status not equal")
	require.Equalf(t, expectedStackStatus.LastAppliedChangeSet, actualStackStatus.LastAppliedChangeSet, "LastAppliedChangeSet in stack status not equal")
	require.Equalf(t, expectedStackStatus.LastAttemptedChangeSet, actualStackStatus.LastAttemptedChangeSet, "LastAttemptedChangeSet in stack status not equal")
	require.Equalf(t, expectedStackStatus.StackName, actualStackStatus.StackName, "StackName in stack status not equal")
	require.Equalf(t, len(expectedStackStatus.Conditions), len(actualStackStatus.Conditions), "Wrong number of conditions in stack status")
	for i, expectedCondition := range expectedStackStatus.Conditions {
		actualCondition := actualStackStatus.Conditions[i]
		require.Equalf(t, expectedCondition.Type, actualCondition.Type, "Type in stack status condition #%d not equal", i+1)
		require.Equalf(t, expectedCondition.Status, actualCondition.Status, "Status in stack status condition #%d not equal", i+1)
		require.Equalf(t, expectedCondition.ObservedGeneration, actualCondition.ObservedGeneration, "ObservedGeneration in stack status condition #%d not equal", i+1)
		require.Equalf(t, expectedCondition.Reason, actualCondition.Reason, "Reason in stack status condition #%d not equal", i+1)
		require.Equalf(t, expectedCondition.Message, actualCondition.Message, "Message in stack status condition #%d not equal", i+1)
	}
}

func generateMockArtifactServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/path.tar.gz" {
			t.Errorf("Expected to request '/path.tar.gz', got: %s", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Errorf("Expected to do a GET request, got: %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		w.Write(mockTestArtifactBytes)
	}))
}

func generateMockGitRepoSource(gitRepo *sourcev1.GitRepository, mockSourceArtifactURL string) {
	gitRepo.Name = mockTemplateGitRepoName
	gitRepo.Namespace = mockSourceNamespace
	gitRepo.Status = sourcev1.GitRepositoryStatus{
		Artifact: &sourcev1.Artifact{
			URL:      mockSourceArtifactURL,
			Revision: mockSourceRevision,
			Checksum: mockTemplateContentsChecksum,
		},
	}
}

func mockS3ClientUpload(s3Client *clientmocks.MockS3Client) {
	s3Client.EXPECT().UploadTemplate(
		mockTemplateUploadBucket,
		"",
		gomock.Any(),
		strings.NewReader(mockTemplateSourceFileContents),
	).Return(mockTemplateS3Url, nil)
}

type expectedEvent struct {
	eventType string
	severity  string
	message   string
}

func TestCfnController_StackManagement(t *testing.T) {
	testCases := map[string]struct {
		fillInInitialCfnStack func(cfnStack *cfnv1.CloudFormationStack)
		fillInSource          func(gitRepo *sourcev1.GitRepository, mockSourceArtifactURL string)
		mockCfnClientCalls    func(cfnClient *clientmocks.MockCloudFormationClient)
		mockS3ClientCalls     func(s3Client *clientmocks.MockS3Client)
		markStackAsInProgress bool
		wantedStackStatus     *cfnv1.CloudFormationStackStatus
		wantedEvent           expectedEvent
		wantedRequeueDelay    time.Duration
	}{
		"create stack if neither stack nor changeset exist": {
			wantedEvent: expectedEvent{
				eventType: "Normal",
				severity:  "info",
				message:   "Creation of stack 'mock-real-stack' in progress (change set arn:aws:cloudformation:us-west-2:111:changeSet/mock-change-set)",
			},
			wantedRequeueDelay: mockPollIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "Unknown",
						ObservedGeneration: mockGenerationId,
						Reason:             "Progressing",
						Message:            "Creation of stack 'mock-real-stack' in progress (change set arn:aws:cloudformation:us-west-2:111:changeSet/mock-change-set)",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			mockS3ClientCalls:     mockS3ClientUpload,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
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
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
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
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			mockCtrl, ctx := gomock.WithContext(context.Background(), t)
			defer mockCtrl.Finish()

			cfnClient := clientmocks.NewMockCloudFormationClient(mockCtrl)
			s3Client := clientmocks.NewMockS3Client(mockCtrl)
			k8sClient := mocks.NewMockClient(mockCtrl)
			k8sStatusWriter := mocks.NewMockStatusWriter(mockCtrl)
			eventRecorder := mocks.NewMockEventRecorder(mockCtrl)
			metricsRecorder := metrics.NewRecorder()
			httpClient := retryablehttp.NewClient()

			server := generateMockArtifactServer(t)
			defer server.Close()
			mockSourceArtifactURL := server.URL + "/path.tar.gz"

			// Mock the initial CFNStack object that the controller will work off of
			k8sClient.EXPECT().Get(
				gomock.Any(),
				mockStackNamespacedName,
				gomock.Any(),
			).DoAndReturn(func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
				cfnStack, ok := obj.(*cfnv1.CloudFormationStack)
				if !ok {
					return errors.New(fmt.Sprintf("Expected a CloudFormationStack object, but got a %T", obj))
				}
				tc.fillInInitialCfnStack(cfnStack)
				return nil
			}).AnyTimes()

			// Mock the source reference
			k8sClient.EXPECT().Get(
				gomock.Any(),
				mockGitSourceReference,
				gomock.Any(),
			).DoAndReturn(func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
				gitRepo, ok := obj.(*sourcev1.GitRepository)
				if !ok {
					return errors.New(fmt.Sprintf("Expected a GitRepository object, but got a %T", obj))
				}
				tc.fillInSource(gitRepo, mockSourceArtifactURL)
				return nil
			})

			// Mock finalizer
			k8sClient.EXPECT().Status().Return(k8sStatusWriter).AnyTimes()
			k8sClient.EXPECT().Patch(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
			).DoAndReturn(func(ctx context.Context, obj ctrlclient.Object, patch ctrlclient.Patch, opts ...ctrlclient.PatchOption) error {
				cfnStack, ok := obj.(*cfnv1.CloudFormationStack)
				if !ok {
					return errors.New(fmt.Sprintf("Expected a CloudFormationStack object, but got a %T", obj))
				}
				finalizers := cfnStack.GetFinalizers()
				require.Equal(t, 1, len(finalizers))
				require.Equal(t, "finalizers.cloudformation.contrib.fluxcd.io", finalizers[0])
				return nil
			})

			// Mock AWS clients
			if tc.mockCfnClientCalls != nil {
				tc.mockCfnClientCalls(cfnClient)
			}
			if tc.mockS3ClientCalls != nil {
				tc.mockS3ClientCalls(s3Client)
			}

			// Validate event recorded
			eventRecorder.EXPECT().AnnotatedEventf(
				gomock.Any(),
				gomock.Any(),
				tc.wantedEvent.eventType,
				tc.wantedEvent.severity,
				tc.wantedEvent.message,
			)

			// Validate the CFN stack object is patched correctly
			finalPatch := k8sStatusWriter.EXPECT().Patch(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
			).DoAndReturn(func(ctx context.Context, obj ctrlclient.Object, patch ctrlclient.Patch, opts ...ctrlclient.PatchOption) error {
				// Stack should be marked as creation in progress
				cfnStack, ok := obj.(*cfnv1.CloudFormationStack)
				if !ok {
					return errors.New(fmt.Sprintf("Expected a CloudFormationStack object, but got a %T", obj))
				}
				compareCfnStackStatus(t, tc.wantedStackStatus, &cfnStack.Status)
				return nil
			})
			if tc.markStackAsInProgress {
				firstPatch := k8sStatusWriter.EXPECT().Patch(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).DoAndReturn(func(ctx context.Context, obj ctrlclient.Object, patch ctrlclient.Patch, opts ...ctrlclient.PatchOption) error {
					// Stack should be marked as reconciliation in progress
					cfnStack, ok := obj.(*cfnv1.CloudFormationStack)
					if !ok {
						return errors.New(fmt.Sprintf("Expected a CloudFormationStack object, but got a %T", obj))
					}
					expectedStackStatus := cfnv1.CloudFormationStackStatus{
						ObservedGeneration: mockGenerationId,
						StackName:          mockRealStackName,
						Conditions: []metav1.Condition{
							{
								Type:               "Ready",
								Status:             "Unknown",
								ObservedGeneration: mockGenerationId,
								Reason:             "Progressing",
								Message:            "Stack reconciliation in progress",
							},
						},
					}
					compareCfnStackStatus(t, &expectedStackStatus, &cfnStack.Status)
					return nil
				})
				gomock.InOrder(
					firstPatch,
					finalPatch,
				)
			}

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
			require.Equal(t, tc.wantedRequeueDelay, result.RequeueAfter)
		})
	}
}
