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

	"github.com/aws/aws-sdk-go-v2/aws"
	sdktypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	cfnv1 "github.com/awslabs/aws-cloudformation-controller-for-flux/api/v1alpha1"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/cloudformation"
	clientmocks "github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/mocks"
	clienttypes "github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/types"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/mocks"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/acl"
	"github.com/fluxcd/pkg/runtime/metrics"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/golang/mock/gomock"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	mockStackName                                     = "mock-stack"
	mockNamespace                                     = "mock-namespace"
	mockGenerationId                                  = 1
	mockGenerationId2                                 = 2
	mockSourceNamespace                               = "mock-namespace"
	mockSourceNamespace2                              = "mock-source-namespace"
	mockRealStackName                                 = "mock-real-stack"
	mockTemplatePath                                  = "template.yaml"
	mockTemplateGitRepoName                           = "mock-cfn-template-git-repo"
	mockTemplateOCIRepoName                           = "mock-cfn-template-oci-repo"
	mockTemplateSourceBucketName                      = "mock-cfn-template-source-bucket"
	mockSourceRevision                                = "mock-source-revision"
	mockSourceRevision2                               = "mock-new-source-revision"
	mockTemplateSourceFile                            = "../../examples/my-cloudformation-templates/template.yaml"
	mockTemplateUploadBucket                          = "mock-template-upload-bucket"
	mockChangeSetName                                 = "flux-1-mock-source-revision"
	mockChangeSetNameNewGeneration                    = "flux-2-mock-source-revision"
	mockChangeSetNameNewSourceRevision                = "flux-1-mock-new-source-revision"
	mockChangeSetArn                                  = "arn:aws:cloudformation:us-west-2:123456789012:changeSet/flux-1-mock-source-revision/uuid"
	mockChangeSetArnNewGeneration                     = "arn:aws:cloudformation:us-west-2:123456789012:changeSet/flux-2-mock-source-revision/uuid"
	mockChangeSetArnNewSourceRevision                 = "arn:aws:cloudformation:us-west-2:123456789012:changeSet/flux-1-mock-new-source-revision/uuid"
	mockChangeSetArnNewSourceRevisionAndNewGeneration = "arn:aws:cloudformation:us-west-2:123456789012:changeSet/flux-2-mock-new-source-revision/uuid"
	mockTemplateS3Url                                 = "https://mock-template-upload-bucket.s3.mock-region.amazonaws.com/mock-flux-template-file-object-key"
)

var (
	scheme = runtime.NewScheme()

	mockStackNamespacedName = types.NamespacedName{
		Name:      mockStackName,
		Namespace: mockNamespace,
	}

	mockStackNamespacedName2 = types.NamespacedName{
		Name:      mockStackName + "2",
		Namespace: mockNamespace + "2",
	}

	mockGitRef = cfnv1.SourceReference{
		Kind:      sourcev1.GitRepositoryKind,
		Name:      mockTemplateGitRepoName,
		Namespace: mockSourceNamespace,
	}
	mockGitRef2 = cfnv1.SourceReference{
		Kind:      sourcev1.GitRepositoryKind,
		Name:      mockTemplateGitRepoName,
		Namespace: mockSourceNamespace2,
	}
	mockGitSourceReference = types.NamespacedName{
		Name:      mockTemplateGitRepoName,
		Namespace: mockSourceNamespace,
	}
	mockGitSourceReference2 = types.NamespacedName{
		Name:      mockTemplateGitRepoName,
		Namespace: mockSourceNamespace2,
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

	mockIntervalDuration, _                = time.ParseDuration("5h")
	mockRetryIntervalDuration, _           = time.ParseDuration("2m")
	mockPollIntervalDuration, _            = time.ParseDuration("30s")
	mockDependencyRetryIntervalDuration, _ = time.ParseDuration("1m")
	mockInterval                           = metav1.Duration{Duration: mockIntervalDuration}
	mockRetryInterval                      = metav1.Duration{Duration: mockRetryIntervalDuration}
	mockPollInterval                       = metav1.Duration{Duration: mockPollIntervalDuration}

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
func compareCfnStackStatus(t *testing.T, kind string, expectedStackStatus *cfnv1.CloudFormationStackStatus, actualStackStatus *cfnv1.CloudFormationStackStatus) {
	require.Equalf(t, expectedStackStatus.ObservedGeneration, actualStackStatus.ObservedGeneration, "ObservedGeneration in %s stack status not equal", kind)
	require.Equalf(t, expectedStackStatus.LastAppliedRevision, actualStackStatus.LastAppliedRevision, "LastAppliedRevision in %s stack status not equal", kind)
	require.Equalf(t, expectedStackStatus.LastAttemptedRevision, actualStackStatus.LastAttemptedRevision, "LastAttemptedRevision in %s stack status not equal", kind)
	require.Equalf(t, expectedStackStatus.LastAppliedChangeSet, actualStackStatus.LastAppliedChangeSet, "LastAppliedChangeSet in %s stack status not equal", kind)
	require.Equalf(t, expectedStackStatus.LastAttemptedChangeSet, actualStackStatus.LastAttemptedChangeSet, "LastAttemptedChangeSet in %s stack status not equal", kind)
	require.Equalf(t, expectedStackStatus.StackName, actualStackStatus.StackName, "StackName in %s stack status not equal", kind)
	require.Equalf(t, len(expectedStackStatus.Conditions), len(actualStackStatus.Conditions), "Wrong number of conditions in %s stack status", kind)
	for i, expectedCondition := range expectedStackStatus.Conditions {
		actualCondition := actualStackStatus.Conditions[i]
		require.Equalf(t, expectedCondition.Type, actualCondition.Type, "Type in %s stack status condition #%d not equal", kind, i+1)
		require.Equalf(t, expectedCondition.Status, actualCondition.Status, "Status in %s stack status condition #%d not equal", kind, i+1)
		require.Equalf(t, expectedCondition.ObservedGeneration, actualCondition.ObservedGeneration, "ObservedGeneration in %s stack status condition #%d not equal", kind, i+1)
		require.Equalf(t, expectedCondition.Reason, actualCondition.Reason, "Reason in %s stack status condition #%d not equal", kind, i+1)
		require.Equalf(t, expectedCondition.Message, actualCondition.Message, "Message in %s stack status condition #%d not equal", kind, i+1)
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

func generateMockGitRepoSource2(gitRepo *sourcev1.GitRepository, mockSourceArtifactURL string) {
	gitRepo.Name = mockTemplateGitRepoName
	gitRepo.Namespace = mockSourceNamespace
	gitRepo.Status = sourcev1.GitRepositoryStatus{
		Artifact: &sourcev1.Artifact{
			URL:      mockSourceArtifactURL,
			Revision: mockSourceRevision2,
			Checksum: mockTemplateContentsChecksum,
		},
	}
}

func generateMockBucketSource(bucket *sourcev1.Bucket, mockSourceArtifactURL string) {
	bucket.Name = mockTemplateGitRepoName
	bucket.Namespace = mockSourceNamespace
	bucket.Status = sourcev1.BucketStatus{
		Artifact: &sourcev1.Artifact{
			URL:      mockSourceArtifactURL,
			Revision: mockSourceRevision,
			Checksum: mockTemplateContentsChecksum,
		},
	}
}

func generateMockOCIRepoSource(ociRepo *sourcev1.OCIRepository, mockSourceArtifactURL string) {
	ociRepo.Name = mockTemplateGitRepoName
	ociRepo.Namespace = mockSourceNamespace
	ociRepo.Status = sourcev1.OCIRepositoryStatus{
		Artifact: &sourcev1.Artifact{
			URL:      mockSourceArtifactURL,
			Revision: mockSourceRevision,
			Checksum: mockTemplateContentsChecksum,
		},
	}
}

func generateStackInput(generation int64, sourceRevision string, changeSetArn string) *clienttypes.Stack {
	return &clienttypes.Stack{
		Name:           mockRealStackName,
		Generation:     generation,
		SourceRevision: sourceRevision,
		ChangeSetArn:   changeSetArn,
		StackConfig: &clienttypes.StackConfig{
			TemplateBucket: mockTemplateUploadBucket,
			TemplateBody:   mockTemplateSourceFileContents,
			Tags: []sdktypes.Tag{
				{
					Key:   aws.String("cfn-controller-test/version"),
					Value: aws.String("v0.0.0"),
				},
				{
					Key:   aws.String("cfn-controller-test/name"),
					Value: aws.String(mockStackName),
				},
				{
					Key:   aws.String("cfn-controller-test/namespace"),
					Value: aws.String(mockNamespace),
				},
			},
		},
	}
}

func generateStackInputWithTemplateUrl(generation int64, sourceRevision string) *clienttypes.Stack {
	input := generateStackInput(generation, sourceRevision, "")
	input.StackConfig.TemplateURL = mockTemplateS3Url
	return input
}

func generateMockCfnStackSpec() cfnv1.CloudFormationStackSpec {
	return cfnv1.CloudFormationStackSpec{
		StackName:              mockRealStackName,
		TemplatePath:           mockTemplatePath,
		SourceRef:              mockGitRef,
		Interval:               mockInterval,
		RetryInterval:          &mockRetryInterval,
		PollInterval:           mockPollInterval,
		Suspend:                false,
		DestroyStackOnDeletion: false,
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

type changeSetStatusPair struct {
	status          sdktypes.ChangeSetStatus
	executionStatus sdktypes.ExecutionStatus
}

type reconciliationLoopTestCase struct {
	cfnStackObjectDoesNotExist bool
	fillInInitialCfnStack      func(cfnStack *cfnv1.CloudFormationStack)
	fillInSource               func(gitRepo *sourcev1.GitRepository, mockSourceArtifactURL string)
	fillInBucket               func(bucket *sourcev1.Bucket, mockSourceArtifactURL string)
	fillInOCIRepository        func(ociRepo *sourcev1.OCIRepository, mockSourceArtifactURL string)
	mockDependencyRetrieval    func(k8sClient *mocks.MockClient)
	mockSourceRetrieval        func(k8sClient *mocks.MockClient)
	mockArtifactServer         func(t *testing.T) *httptest.Server
	mockCfnClientCalls         func(cfnClient *clientmocks.MockCloudFormationClient)
	mockS3ClientCalls          func(s3Client *clientmocks.MockS3Client)
	markStackAsInProgress      bool
	removeFinalizers           bool
	wantedStackStatus          *cfnv1.CloudFormationStackStatus
	wantedEvents               []*expectedEvent
	wantedRequeueDelay         time.Duration
	wantedErr                  error
}

func runReconciliationLoopTestCase(t *testing.T, tc *reconciliationLoopTestCase) {
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

	var server *httptest.Server
	if tc.mockArtifactServer != nil {
		server = tc.mockArtifactServer(t)
	} else {
		server = generateMockArtifactServer(t)
	}
	defer server.Close()
	mockSourceArtifactURL := server.URL + "/path.tar.gz"

	// Mock the initial CFNStack object that the controller will work off of
	if tc.cfnStackObjectDoesNotExist {
		k8sClient.EXPECT().Get(
			gomock.Any(),
			mockStackNamespacedName,
			gomock.Any(),
		).Return(
			&apierrors.StatusError{
				ErrStatus: metav1.Status{
					Status:  metav1.StatusFailure,
					Reason:  metav1.StatusReasonNotFound,
					Message: "hello world",
				}},
		)
	} else {
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
	}

	// Mock a stack dependency
	if tc.mockDependencyRetrieval != nil {
		tc.mockDependencyRetrieval(k8sClient)
	}

	// Mock the source reference
	if tc.mockSourceRetrieval != nil {
		tc.mockSourceRetrieval(k8sClient)
	}
	if tc.fillInSource != nil {
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
	}
	if tc.fillInBucket != nil {
		k8sClient.EXPECT().Get(
			gomock.Any(),
			mockBucketSourceReference,
			gomock.Any(),
		).DoAndReturn(func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
			bucket, ok := obj.(*sourcev1.Bucket)
			if !ok {
				return errors.New(fmt.Sprintf("Expected a Bucket object, but got a %T", obj))
			}
			tc.fillInBucket(bucket, mockSourceArtifactURL)
			return nil
		})
	}
	if tc.fillInOCIRepository != nil {
		k8sClient.EXPECT().Get(
			gomock.Any(),
			mockOCISourceReference,
			gomock.Any(),
		).DoAndReturn(func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
			ociRepo, ok := obj.(*sourcev1.OCIRepository)
			if !ok {
				return errors.New(fmt.Sprintf("Expected an OCIRepository object, but got a %T", obj))
			}
			tc.fillInOCIRepository(ociRepo, mockSourceArtifactURL)
			return nil
		})
	}

	// Mock adding the finalizer
	if !tc.cfnStackObjectDoesNotExist {
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
	}

	// Mock AWS clients
	if tc.mockCfnClientCalls != nil {
		tc.mockCfnClientCalls(cfnClient)
	}
	if tc.mockS3ClientCalls != nil {
		tc.mockS3ClientCalls(s3Client)
	}

	// Validate event recorded
	if tc.wantedEvents != nil {
		for _, event := range tc.wantedEvents {
			eventRecorder.EXPECT().AnnotatedEventf(
				gomock.Any(),
				gomock.Any(),
				event.eventType,
				event.severity,
				event.message,
			)
		}
	}

	// Mock removing the finalizer
	if tc.removeFinalizers {
		k8sClient.EXPECT().Update(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
		).DoAndReturn(func(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.UpdateOption) error {
			cfnStack, ok := obj.(*cfnv1.CloudFormationStack)
			if !ok {
				return errors.New(fmt.Sprintf("Expected a CloudFormationStack object, but got a %T", obj))
			}
			finalizers := cfnStack.GetFinalizers()
			require.Empty(t, finalizers)
			compareCfnStackStatus(t, "final", tc.wantedStackStatus, &cfnStack.Status)
			return nil
		})
	} else if tc.wantedStackStatus != nil {
		// Validate the CFN stack object is patched correctly
		finalPatch := k8sStatusWriter.EXPECT().Patch(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			ctrlclient.FieldOwner("cfn-controller-test"),
		).DoAndReturn(func(ctx context.Context, obj ctrlclient.Object, patch ctrlclient.Patch, opts ...ctrlclient.PatchOption) error {
			// Stack should be marked as creation in progress
			cfnStack, ok := obj.(*cfnv1.CloudFormationStack)
			if !ok {
				return errors.New(fmt.Sprintf("Expected a CloudFormationStack object, but got a %T", obj))
			}
			compareCfnStackStatus(t, "final", tc.wantedStackStatus, &cfnStack.Status)
			return nil
		})
		if tc.markStackAsInProgress {
			firstPatch := k8sStatusWriter.EXPECT().Patch(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				ctrlclient.FieldOwner("cfn-controller-test"),
			).DoAndReturn(func(ctx context.Context, obj ctrlclient.Object, patch ctrlclient.Patch, opts ...ctrlclient.PatchOption) error {
				// Stack should be marked as reconciliation in progress
				cfnStack, ok := obj.(*cfnv1.CloudFormationStack)
				if !ok {
					return errors.New(fmt.Sprintf("Expected a CloudFormationStack object, but got a %T", obj))
				}

				expectedStack := cfnv1.CloudFormationStack{}
				tc.fillInInitialCfnStack(&expectedStack)
				expectedStack.Status.ObservedGeneration = expectedStack.Generation
				expectedStack.Status.StackName = mockRealStackName
				expectedStack.Status.Conditions = []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "Unknown",
						ObservedGeneration: expectedStack.Generation,
						Reason:             "Progressing",
						Message:            "Stack reconciliation in progress",
					},
				}
				compareCfnStackStatus(t, "initial", &expectedStack.Status, &cfnStack.Status)
				return nil
			})
			gomock.InOrder(
				firstPatch,
				finalPatch,
			)
		}
	}

	reconciler := &CloudFormationStackReconciler{
		Scheme:              scheme,
		Client:              k8sClient,
		CfnClient:           cfnClient,
		S3Client:            s3Client,
		TemplateBucket:      mockTemplateUploadBucket,
		EventRecorder:       eventRecorder,
		MetricsRecorder:     metricsRecorder,
		ControllerName:      "cfn-controller-test",
		ControllerVersion:   "v0.0.0",
		NoCrossNamespaceRef: true,
		httpClient:          httpClient,
		requeueDependency:   mockDependencyRetryIntervalDuration,
	}

	request := ctrl.Request{NamespacedName: mockStackNamespacedName}

	// WHEN
	result, err := reconciler.Reconcile(ctx, request)

	// THEN
	require.Equal(t, tc.wantedErr, err)
	require.False(t, result.Requeue)
	require.Equal(t, tc.wantedRequeueDelay, result.RequeueAfter)
}

func TestCfnController_ReconcileStack(t *testing.T) {
	testCases := map[string]*reconciliationLoopTestCase{
		"no reconciliation if stack is suspended": {
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.Suspend = true
			},
		},
		"no reconciliation if stack dependencies are not ready": {
			wantedEvents: []*expectedEvent{{
				eventType: "Normal",
				severity:  "info",
				message:   "Dependencies do not meet ready condition (dependency 'mock-namespace2/mock-stack2' is not ready)",
			}},
			wantedRequeueDelay: mockDependencyRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "DependencyNotReady",
						Message:            "Dependencies do not meet ready condition (dependency 'mock-namespace2/mock-stack2' is not ready)",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.DependsOn = []meta.NamespacedObjectReference{
					{
						Name:      mockStackNamespacedName2.Name,
						Namespace: mockStackNamespacedName2.Namespace,
					},
				}
			},
			mockDependencyRetrieval: func(k8sClient *mocks.MockClient) {
				k8sClient.EXPECT().Get(
					gomock.Any(),
					mockStackNamespacedName2,
					gomock.Any(),
				).DoAndReturn(func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
					cfnStack, ok := obj.(*cfnv1.CloudFormationStack)
					if !ok {
						return errors.New(fmt.Sprintf("Expected a CloudFormationStack object, but got a %T", obj))
					}
					cfnStack.Name = mockStackNamespacedName2.Name
					cfnStack.Namespace = mockStackNamespacedName2.Namespace
					cfnStack.Generation = mockGenerationId
					cfnStack.Status = cfnv1.CloudFormationStackStatus{
						ObservedGeneration: mockGenerationId,
						Conditions: []metav1.Condition{
							{
								Type:               "Ready",
								Status:             "False",
								ObservedGeneration: mockGenerationId,
								Reason:             "HelloWorld",
								Message:            "Hello world",
							},
						},
					}
					return nil
				})
			},
		},
		"no reconciliation if stack dependencies are not reconciled": {
			wantedEvents: []*expectedEvent{{
				eventType: "Normal",
				severity:  "info",
				message:   "Dependencies do not meet ready condition (dependency 'mock-namespace2/mock-stack2' is not ready)",
			}},
			wantedRequeueDelay: mockDependencyRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "DependencyNotReady",
						Message:            "Dependencies do not meet ready condition (dependency 'mock-namespace2/mock-stack2' is not ready)",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.DependsOn = []meta.NamespacedObjectReference{
					{
						Name:      mockStackNamespacedName2.Name,
						Namespace: mockStackNamespacedName2.Namespace,
					},
				}
			},
			mockDependencyRetrieval: func(k8sClient *mocks.MockClient) {
				k8sClient.EXPECT().Get(
					gomock.Any(),
					mockStackNamespacedName2,
					gomock.Any(),
				).DoAndReturn(func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
					cfnStack, ok := obj.(*cfnv1.CloudFormationStack)
					if !ok {
						return errors.New(fmt.Sprintf("Expected a CloudFormationStack object, but got a %T", obj))
					}
					cfnStack.Name = mockStackNamespacedName2.Name
					cfnStack.Namespace = mockStackNamespacedName2.Namespace
					cfnStack.Generation = mockGenerationId
					cfnStack.Status = cfnv1.CloudFormationStackStatus{}
					return nil
				})
			},
		},
		"create stack if neither stack nor changeset exist": {
			wantedEvents: []*expectedEvent{{
				eventType: "Normal",
				severity:  "info",
				message:   fmt.Sprintf("Creation of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArn),
			}},
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
						Message:            fmt.Sprintf("Creation of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArn),
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
				cfnStack.Spec = generateMockCfnStackSpec()
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(nil, &cloudformation.ErrStackNotFound{})

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetNotFound{})

				expectedCreateStackIn := generateStackInputWithTemplateUrl(mockGenerationId, mockSourceRevision)
				cfnClient.EXPECT().CreateStack(expectedCreateStackIn).Return(mockChangeSetArn, nil)
			},
		},
		"reconcile stack if dependencies are ready": {
			wantedEvents: []*expectedEvent{{
				eventType: "Normal",
				severity:  "info",
				message:   fmt.Sprintf("Creation of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArn),
			}},
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
						Message:            fmt.Sprintf("Creation of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArn),
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
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.DependsOn = []meta.NamespacedObjectReference{
					{
						Name:      mockStackNamespacedName2.Name,
						Namespace: mockStackNamespacedName2.Namespace,
					},
				}
			},
			mockDependencyRetrieval: func(k8sClient *mocks.MockClient) {
				k8sClient.EXPECT().Get(
					gomock.Any(),
					mockStackNamespacedName2,
					gomock.Any(),
				).DoAndReturn(func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
					cfnStack, ok := obj.(*cfnv1.CloudFormationStack)
					if !ok {
						return errors.New(fmt.Sprintf("Expected a CloudFormationStack object, but got a %T", obj))
					}
					cfnStack.Name = mockStackNamespacedName2.Name
					cfnStack.Namespace = mockStackNamespacedName2.Namespace
					cfnStack.Generation = mockGenerationId
					cfnStack.Status = cfnv1.CloudFormationStackStatus{
						ObservedGeneration:     mockGenerationId,
						StackName:              mockRealStackName,
						LastAttemptedRevision:  mockSourceRevision,
						LastAttemptedChangeSet: mockChangeSetArn,
						Conditions: []metav1.Condition{
							{
								Type:               "Ready",
								Status:             "True",
								ObservedGeneration: mockGenerationId,
								Reason:             "Succeeded",
								Message:            "Hello world",
							},
						},
					}
					return nil
				})
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(nil, &cloudformation.ErrStackNotFound{})

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetNotFound{})

				expectedCreateStackIn := generateStackInputWithTemplateUrl(mockGenerationId, mockSourceRevision)
				cfnClient.EXPECT().CreateStack(expectedCreateStackIn).Return(mockChangeSetArn, nil)
			},
		},
		"create stack with parameters": {
			wantedEvents: []*expectedEvent{{
				eventType: "Normal",
				severity:  "info",
				message:   fmt.Sprintf("Creation of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArn),
			}},
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
						Message:            fmt.Sprintf("Creation of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArn),
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
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.StackParameters = []cfnv1.StackParameter{
					{
						Key:   "ParamKey",
						Value: "ParamValue",
					},
				}
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				expectedDescribeStackIn.Parameters = []sdktypes.Parameter{
					{
						ParameterKey:   aws.String("ParamKey"),
						ParameterValue: aws.String("ParamValue"),
					},
				}
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(nil, &cloudformation.ErrStackNotFound{})

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				expectedDescribeChangeSetIn.Parameters = expectedDescribeStackIn.Parameters
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetNotFound{})

				expectedCreateStackIn := generateStackInputWithTemplateUrl(mockGenerationId, mockSourceRevision)
				expectedCreateStackIn.Parameters = expectedDescribeStackIn.Parameters
				cfnClient.EXPECT().CreateStack(expectedCreateStackIn).Return(mockChangeSetArn, nil)
			},
		},
		"update stack with parameters": {
			wantedEvents: []*expectedEvent{{
				eventType: "Normal",
				severity:  "info",
				message:   fmt.Sprintf("Update of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArnNewGeneration),
			}},
			wantedRequeueDelay: mockPollIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId2,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision,
				LastAppliedRevision:    mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArnNewGeneration,
				LastAppliedChangeSet:   mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "Unknown",
						ObservedGeneration: mockGenerationId2,
						Reason:             "Progressing",
						Message:            fmt.Sprintf("Update of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArnNewGeneration),
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			mockS3ClientCalls:     mockS3ClientUpload,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId2
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.StackParameters = []cfnv1.StackParameter{
					{
						Key:   "ParamKey",
						Value: "ParamValue",
					},
				}
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision,
					LastAppliedRevision:    mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArn,
					LastAppliedChangeSet:   mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "True",
							ObservedGeneration: mockGenerationId,
							Reason:             "Hello",
							Message:            "World",
						},
					},
				}
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId2, mockSourceRevision, "")
				expectedDescribeStackIn.Parameters = []sdktypes.Parameter{
					{
						ParameterKey:   aws.String("ParamKey"),
						ParameterValue: aws.String("ParamValue"),
					},
				}
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:         aws.String(mockRealStackName),
					StackStatus:       sdktypes.StackStatusCreateComplete,
					StackStatusReason: aws.String("hello world"),
				}, nil)

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId2, mockSourceRevision, "")
				expectedDescribeChangeSetIn.Parameters = expectedDescribeStackIn.Parameters
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetNotFound{})

				expectedUpdateStackIn := generateStackInputWithTemplateUrl(mockGenerationId2, mockSourceRevision)
				expectedUpdateStackIn.Parameters = expectedDescribeStackIn.Parameters
				cfnClient.EXPECT().UpdateStack(expectedUpdateStackIn).Return(mockChangeSetArnNewGeneration, nil)
			},
		},
		"create stack with tags": {
			wantedEvents: []*expectedEvent{{
				eventType: "Normal",
				severity:  "info",
				message:   fmt.Sprintf("Creation of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArn),
			}},
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
						Message:            fmt.Sprintf("Creation of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArn),
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
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.StackTags = []cfnv1.StackTag{
					{
						Key:   "TagKey",
						Value: "TagValue",
					},
				}
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				expectedDescribeStackIn.Tags = append(expectedDescribeStackIn.Tags,
					sdktypes.Tag{
						Key:   aws.String("TagKey"),
						Value: aws.String("TagValue"),
					},
				)
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(nil, &cloudformation.ErrStackNotFound{})

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				expectedDescribeChangeSetIn.Tags = expectedDescribeStackIn.Tags
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetNotFound{})

				expectedCreateStackIn := generateStackInputWithTemplateUrl(mockGenerationId, mockSourceRevision)
				expectedCreateStackIn.Tags = expectedDescribeStackIn.Tags
				cfnClient.EXPECT().CreateStack(expectedCreateStackIn).Return(mockChangeSetArn, nil)
			},
		},
		"update stack with tags": {
			wantedEvents: []*expectedEvent{{
				eventType: "Normal",
				severity:  "info",
				message:   fmt.Sprintf("Update of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArnNewGeneration),
			}},
			wantedRequeueDelay: mockPollIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId2,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision,
				LastAppliedRevision:    mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArnNewGeneration,
				LastAppliedChangeSet:   mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "Unknown",
						ObservedGeneration: mockGenerationId2,
						Reason:             "Progressing",
						Message:            fmt.Sprintf("Update of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArnNewGeneration),
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			mockS3ClientCalls:     mockS3ClientUpload,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId2
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.StackTags = []cfnv1.StackTag{
					{
						Key:   "TagKey",
						Value: "TagValue",
					},
				}
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision,
					LastAppliedRevision:    mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArn,
					LastAppliedChangeSet:   mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "True",
							ObservedGeneration: mockGenerationId,
							Reason:             "Hello",
							Message:            "World",
						},
					},
				}
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId2, mockSourceRevision, "")
				expectedDescribeStackIn.Tags = append(expectedDescribeStackIn.Tags,
					sdktypes.Tag{
						Key:   aws.String("TagKey"),
						Value: aws.String("TagValue"),
					},
				)
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:         aws.String(mockRealStackName),
					StackStatus:       sdktypes.StackStatusCreateComplete,
					StackStatusReason: aws.String("hello world"),
				}, nil)

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId2, mockSourceRevision, "")
				expectedDescribeChangeSetIn.Tags = expectedDescribeStackIn.Tags
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetNotFound{})

				expectedUpdateStackIn := generateStackInputWithTemplateUrl(mockGenerationId2, mockSourceRevision)
				expectedUpdateStackIn.Tags = expectedDescribeStackIn.Tags
				cfnClient.EXPECT().UpdateStack(expectedUpdateStackIn).Return(mockChangeSetArnNewGeneration, nil)
			},
		},
		"continue stack rollback if the real stack has UPDATE_ROLLBACK_FAILED status": {
			wantedEvents: []*expectedEvent{{
				eventType: "Warning",
				severity:  "error",
				message:   "Stack 'mock-real-stack' has a previously failed rollback (status 'UPDATE_ROLLBACK_FAILED'), continuing rollback",
			}},
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "StackRollbackFailed",
						Message:            "Stack 'mock-real-stack' has a previously failed rollback (status 'UPDATE_ROLLBACK_FAILED'), continuing rollback",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:   aws.String(mockRealStackName),
					StackStatus: sdktypes.StackStatusUpdateRollbackFailed,
				}, nil)

				cfnClient.EXPECT().ContinueStackRollback(expectedDescribeStackIn).Return(nil)
			},
		},
	}

	// Test cases when stack is in progress
	inProgressStackStatuses := []sdktypes.StackStatus{
		sdktypes.StackStatusCreateInProgress,
		sdktypes.StackStatusDeleteInProgress,
		sdktypes.StackStatusRollbackInProgress,
		sdktypes.StackStatusUpdateCompleteCleanupInProgress,
		sdktypes.StackStatusUpdateInProgress,
		sdktypes.StackStatusUpdateRollbackCompleteCleanupInProgress,
		sdktypes.StackStatusUpdateRollbackInProgress,
		sdktypes.StackStatusImportInProgress,
		sdktypes.StackStatusImportRollbackInProgress,
	}
	for _, stackStatus := range inProgressStackStatuses {
		expectedStackStatus := stackStatus
		testCases[fmt.Sprintf("set stack as in-progress if the real stack has %s status", expectedStackStatus)] =
			&reconciliationLoopTestCase{
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
							Message:            fmt.Sprintf("Stack action for stack '%s' is in progress (status: '%s'), waiting for stack action to complete", mockRealStackName, expectedStackStatus),
						},
					},
				},
				fillInSource: generateMockGitRepoSource,
				fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
					cfnStack.Name = mockStackName
					cfnStack.Namespace = mockNamespace
					cfnStack.Generation = mockGenerationId
					cfnStack.Spec = generateMockCfnStackSpec()
					cfnStack.Status = cfnv1.CloudFormationStackStatus{
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
								Message:            "Hello world",
							},
						},
					}
				},
				mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
					expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, mockChangeSetArn)
					cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
						StackName:   aws.String(mockRealStackName),
						StackStatus: expectedStackStatus,
					}, nil)
				},
			}
	}

	unrecoverableFailureStackStatuses := []sdktypes.StackStatus{
		sdktypes.StackStatusCreateFailed,
		sdktypes.StackStatusDeleteFailed,
		sdktypes.StackStatusRollbackComplete,
		sdktypes.StackStatusRollbackFailed,
	}
	for _, stackStatus := range unrecoverableFailureStackStatuses {
		expectedStackStatus := stackStatus
		expectedStatusMessage := fmt.Sprintf("Stack 'mock-real-stack' is in an unrecoverable state and must be recreated: status '%s', reason 'hello world'", expectedStackStatus)

		testCases[fmt.Sprintf("delete the real stack if the real stack has %s status", expectedStackStatus)] =
			&reconciliationLoopTestCase{
				wantedEvents: []*expectedEvent{{
					eventType: "Warning",
					severity:  "error",
					message:   expectedStatusMessage,
				}},
				wantedRequeueDelay: mockRetryIntervalDuration,
				wantedStackStatus: &cfnv1.CloudFormationStackStatus{
					ObservedGeneration:    mockGenerationId,
					StackName:             mockRealStackName,
					LastAttemptedRevision: mockSourceRevision,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "False",
							ObservedGeneration: mockGenerationId,
							Reason:             "UnrecoverableStackFailure",
							Message:            expectedStatusMessage,
						},
					},
				},
				markStackAsInProgress: true,
				fillInSource:          generateMockGitRepoSource,
				fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
					cfnStack.Name = mockStackName
					cfnStack.Namespace = mockNamespace
					cfnStack.Generation = mockGenerationId
					cfnStack.Spec = generateMockCfnStackSpec()
				},
				mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
					expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
					cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
						StackName:         aws.String(mockRealStackName),
						StackStatus:       expectedStackStatus,
						StackStatusReason: aws.String("hello world"),
					}, nil)

					cfnClient.EXPECT().DeleteStack(expectedDescribeStackIn).Return(nil)
				},
			}
	}

	successfulDeploymentStackStatuses := []sdktypes.StackStatus{
		sdktypes.StackStatusCreateComplete,
		sdktypes.StackStatusUpdateComplete,
		sdktypes.StackStatusImportComplete,
	}
	recoverableFailureStackStatuses := []sdktypes.StackStatus{
		sdktypes.StackStatusUpdateFailed,
		sdktypes.StackStatusUpdateRollbackComplete,
		sdktypes.StackStatusImportRollbackComplete,
		sdktypes.StackStatusImportRollbackFailed,
	}
	idleStackStatuses := append(successfulDeploymentStackStatuses, recoverableFailureStackStatuses...)
	for _, stackStatus := range idleStackStatuses {
		expectedStackStatus := stackStatus

		expectedStatusMsgNewGeneration := fmt.Sprintf("Update of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArnNewGeneration)
		changeSetDoesNotExistNewGenerationTC := &reconciliationLoopTestCase{
			wantedEvents:       []*expectedEvent{},
			wantedRequeueDelay: mockPollIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId2,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision,
				LastAppliedRevision:    mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArnNewGeneration,
				LastAppliedChangeSet:   mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "Unknown",
						ObservedGeneration: mockGenerationId2,
						Reason:             "Progressing",
						Message:            expectedStatusMsgNewGeneration,
					},
				},
			},
			fillInSource: generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId2
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision,
					LastAppliedRevision:    mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArn,
					LastAppliedChangeSet:   mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "True",
							ObservedGeneration: mockGenerationId,
							Reason:             "Hello",
							Message:            "World",
						},
					},
				}
			},
			markStackAsInProgress: true,
			mockS3ClientCalls:     mockS3ClientUpload,
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId2, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:         aws.String(mockRealStackName),
					StackStatus:       expectedStackStatus,
					StackStatusReason: aws.String("hello world"),
				}, nil)

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId2, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetNotFound{})

				expectedUpdateStackIn := generateStackInputWithTemplateUrl(mockGenerationId2, mockSourceRevision)
				cfnClient.EXPECT().UpdateStack(expectedUpdateStackIn).Return(mockChangeSetArnNewGeneration, nil)
			},
		}

		for _, status := range recoverableFailureStackStatuses {
			if expectedStackStatus == status {
				msg := fmt.Sprintf("Stack 'mock-real-stack' is in a failed state (status '%s', reason '%s'), creating a new change set", expectedStackStatus, "hello world")
				changeSetDoesNotExistNewGenerationTC.wantedEvents = append(changeSetDoesNotExistNewGenerationTC.wantedEvents, &expectedEvent{
					eventType: "Warning",
					severity:  "error",
					message:   msg,
				})
				break
			}
		}
		changeSetDoesNotExistNewGenerationTC.wantedEvents = append(changeSetDoesNotExistNewGenerationTC.wantedEvents, &expectedEvent{
			eventType: "Normal",
			severity:  "info",
			message:   fmt.Sprintf("Update of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArnNewGeneration),
		})

		testCases[fmt.Sprintf("update the real stack if the real stack has %s status and the desired change set does not exist due to a new generation", expectedStackStatus)] = changeSetDoesNotExistNewGenerationTC

		expectedStatusMsgNewSourceRevision := fmt.Sprintf("Update of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArnNewSourceRevision)
		changeSetDoesNotExistNewSourceRevisionTC := &reconciliationLoopTestCase{
			wantedEvents:       []*expectedEvent{},
			wantedRequeueDelay: mockPollIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision2,
				LastAppliedRevision:    mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArnNewSourceRevision,
				LastAppliedChangeSet:   mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "Unknown",
						ObservedGeneration: mockGenerationId,
						Reason:             "Progressing",
						Message:            expectedStatusMsgNewSourceRevision,
					},
				},
			},
			fillInSource: generateMockGitRepoSource2,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision,
					LastAppliedRevision:    mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArn,
					LastAppliedChangeSet:   mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "True",
							ObservedGeneration: mockGenerationId,
							Reason:             "Hello",
							Message:            "World",
						},
					},
				}
			},
			markStackAsInProgress: false,
			mockS3ClientCalls:     mockS3ClientUpload,
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision2, "")
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:         aws.String(mockRealStackName),
					StackStatus:       expectedStackStatus,
					StackStatusReason: aws.String("hello world"),
				}, nil)

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId, mockSourceRevision2, "")
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetNotFound{})

				expectedUpdateStackIn := generateStackInputWithTemplateUrl(mockGenerationId, mockSourceRevision2)
				cfnClient.EXPECT().UpdateStack(expectedUpdateStackIn).Return(mockChangeSetArnNewSourceRevision, nil)
			},
		}

		for _, status := range recoverableFailureStackStatuses {
			if expectedStackStatus == status {
				msg := fmt.Sprintf("Stack 'mock-real-stack' is in a failed state (status '%s', reason '%s'), creating a new change set", expectedStackStatus, "hello world")
				changeSetDoesNotExistNewSourceRevisionTC.wantedEvents = append(changeSetDoesNotExistNewSourceRevisionTC.wantedEvents, &expectedEvent{
					eventType: "Warning",
					severity:  "error",
					message:   msg,
				})
				break
			}
		}
		changeSetDoesNotExistNewSourceRevisionTC.wantedEvents = append(changeSetDoesNotExistNewSourceRevisionTC.wantedEvents, &expectedEvent{
			eventType: "Normal",
			severity:  "info",
			message:   fmt.Sprintf("Update of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArnNewSourceRevision),
		})

		testCases[fmt.Sprintf("update the real stack if the real stack has %s status and the desired change set does not exist due to a new source revision", expectedStackStatus)] = changeSetDoesNotExistNewSourceRevisionTC
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			runReconciliationLoopTestCase(t, tc)
		})
	}
}

func TestCfnController_ReconcileSource(t *testing.T) {
	testCases := map[string]*reconciliationLoopTestCase{
		"use an S3 bucket source": {
			wantedEvents: []*expectedEvent{{
				eventType: "Normal",
				severity:  "info",
				message:   fmt.Sprintf("Creation of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArn),
			}},
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
						Message:            fmt.Sprintf("Creation of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArn),
					},
				},
			},
			markStackAsInProgress: true,
			fillInBucket:          generateMockBucketSource,
			mockS3ClientCalls:     mockS3ClientUpload,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.SourceRef = mockBucketSourceRef
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(nil, &cloudformation.ErrStackNotFound{})

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetNotFound{})

				expectedCreateStackIn := generateStackInputWithTemplateUrl(mockGenerationId, mockSourceRevision)
				cfnClient.EXPECT().CreateStack(expectedCreateStackIn).Return(mockChangeSetArn, nil)
			},
		},
		"use an OCI Repository source": {
			wantedEvents: []*expectedEvent{{
				eventType: "Normal",
				severity:  "info",
				message:   fmt.Sprintf("Creation of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArn),
			}},
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
						Message:            fmt.Sprintf("Creation of stack 'mock-real-stack' in progress (change set %s)", mockChangeSetArn),
					},
				},
			},
			markStackAsInProgress: true,
			fillInOCIRepository:   generateMockOCIRepoSource,
			mockS3ClientCalls:     mockS3ClientUpload,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.SourceRef = mockOCIRef
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(nil, &cloudformation.ErrStackNotFound{})

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetNotFound{})

				expectedCreateStackIn := generateStackInputWithTemplateUrl(mockGenerationId, mockSourceRevision)
				cfnClient.EXPECT().CreateStack(expectedCreateStackIn).Return(mockChangeSetArn, nil)
			},
		},
		"reject source in another namespace": {
			wantedErr:          acl.AccessDeniedError("can't access 'GitRepository/mock-source-namespace/mock-cfn-template-git-repo', cross-namespace references have been blocked"),
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration: mockGenerationId,
				StackName:          mockRealStackName,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "ArtifactFailed",
						Message:            fmt.Sprintf("Failed to resolve source 'GitRepository/mock-source-namespace/mock-cfn-template-git-repo'"),
					},
				},
			},
			markStackAsInProgress: true,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.SourceRef = mockGitRef2
			},
		},
		"git source cannot be found": {
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration: mockGenerationId,
				StackName:          mockRealStackName,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "ArtifactFailed",
						Message:            fmt.Sprintf("Source 'GitRepository/%s/%s' not found", mockSourceNamespace, mockTemplateGitRepoName),
					},
				},
			},
			markStackAsInProgress: true,
			mockSourceRetrieval: func(k8sClient *mocks.MockClient) {
				k8sClient.EXPECT().Get(
					gomock.Any(),
					mockGitSourceReference,
					gomock.Any(),
				).Return(
					&apierrors.StatusError{
						ErrStatus: metav1.Status{
							Status:  metav1.StatusFailure,
							Reason:  metav1.StatusReasonNotFound,
							Message: "hello world",
						}},
				)
			},
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
			},
		},
		"bucket source cannot be found": {
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration: mockGenerationId,
				StackName:          mockRealStackName,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "ArtifactFailed",
						Message:            fmt.Sprintf("Source 'Bucket/%s/%s' not found", mockSourceNamespace, mockTemplateSourceBucketName),
					},
				},
			},
			markStackAsInProgress: true,
			mockSourceRetrieval: func(k8sClient *mocks.MockClient) {
				k8sClient.EXPECT().Get(
					gomock.Any(),
					mockBucketSourceReference,
					gomock.Any(),
				).Return(
					&apierrors.StatusError{
						ErrStatus: metav1.Status{
							Status:  metav1.StatusFailure,
							Reason:  metav1.StatusReasonNotFound,
							Message: "hello world",
						}},
				)
			},
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.SourceRef = mockBucketSourceRef
			},
		},
		"OCI repository source cannot be found": {
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration: mockGenerationId,
				StackName:          mockRealStackName,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "ArtifactFailed",
						Message:            fmt.Sprintf("Source 'OCIRepository/%s/%s' not found", mockSourceNamespace, mockTemplateOCIRepoName),
					},
				},
			},
			markStackAsInProgress: true,
			mockSourceRetrieval: func(k8sClient *mocks.MockClient) {
				k8sClient.EXPECT().Get(
					gomock.Any(),
					mockOCISourceReference,
					gomock.Any(),
				).Return(
					&apierrors.StatusError{
						ErrStatus: metav1.Status{
							Status:  metav1.StatusFailure,
							Reason:  metav1.StatusReasonNotFound,
							Message: "hello world",
						}},
				)
			},
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.SourceRef = mockOCIRef
			},
		},
		"unsupported source type": {
			wantedErr:          fmt.Errorf("source `%s` kind 'HelmRepository' not supported", mockTemplateOCIRepoName),
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration: mockGenerationId,
				StackName:          mockRealStackName,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "ArtifactFailed",
						Message:            fmt.Sprintf("Failed to resolve source 'HelmRepository/%s/%s'", mockSourceNamespace, mockTemplateOCIRepoName),
					},
				},
			},
			markStackAsInProgress: true,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.SourceRef = cfnv1.SourceReference{
					Kind:      sourcev1.HelmRepositoryKind,
					Name:      mockTemplateOCIRepoName,
					Namespace: mockSourceNamespace,
				}
			},
		},
		"source artifact is not yet ready": {
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration: mockGenerationId,
				StackName:          mockRealStackName,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "ArtifactFailed",
						Message:            "Source 'GitRepository/mock-namespace/mock-cfn-template-git-repo' is not ready, artifact not found",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource: func(gitRepo *sourcev1.GitRepository, mockSourceArtifactURL string) {
				gitRepo.Name = mockTemplateGitRepoName
				gitRepo.Namespace = mockSourceNamespace
				gitRepo.Status = sourcev1.GitRepositoryStatus{}
			},
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
			},
		},
		"fail to download artifact from the source controller": {
			wantedErr:          fmt.Errorf("failed to download artifact, status code: 404 Not Found"),
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "ArtifactFailed",
						Message:            "Failed to load template 'template.yaml' from source 'GitRepository/mock-namespace/mock-cfn-template-git-repo'",
					},
				},
			},
			markStackAsInProgress: true,
			mockArtifactServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			fillInSource: generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
			},
		},
		"downloaded artifact does not match its checksum": {
			wantedErr:          fmt.Errorf("failed to verify artifact: computed checksum '%s' doesn't match advertised 'hello world'", mockTemplateContentsChecksum),
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "ArtifactFailed",
						Message:            "Failed to load template 'template.yaml' from source 'GitRepository/mock-namespace/mock-cfn-template-git-repo'",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource: func(gitRepo *sourcev1.GitRepository, mockSourceArtifactURL string) {
				gitRepo.Name = mockTemplateGitRepoName
				gitRepo.Namespace = mockSourceNamespace
				gitRepo.Status = sourcev1.GitRepositoryStatus{
					Artifact: &sourcev1.Artifact{
						URL:      mockSourceArtifactURL,
						Revision: mockSourceRevision,
						Checksum: "hello world",
					},
				}
			},
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
			},
		},
		"specified template file path cannot be found in the artifact": {
			wantedErr:          fmt.Errorf("unable to read template file 'does-not-exist.yaml' in the artifact temp directory"),
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "ArtifactFailed",
						Message:            "Failed to load template 'does-not-exist.yaml' from source 'GitRepository/mock-namespace/mock-cfn-template-git-repo'",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.TemplatePath = "does-not-exist.yaml"
			},
		},
		"cannot traverse paths outside of the artifact": {
			wantedErr:          fmt.Errorf("unable to read template file '../../usr/bin/ls' in the artifact temp directory"),
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "ArtifactFailed",
						Message:            "Failed to load template '../../usr/bin/ls' from source 'GitRepository/mock-namespace/mock-cfn-template-git-repo'",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.TemplatePath = "../../usr/bin/ls"
			},
		},
		"cannot provide absolute file paths for the template": {
			wantedErr:          fmt.Errorf("unable to read template file '/usr/bin/ls' in the artifact temp directory"),
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "ArtifactFailed",
						Message:            "Failed to load template '/usr/bin/ls' from source 'GitRepository/mock-namespace/mock-cfn-template-git-repo'",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.TemplatePath = "/usr/bin/ls"
			},
		},
		"cannot provide directories for the template": {
			wantedErr:          fmt.Errorf("unable to read template file './' in the artifact temp directory"),
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "ArtifactFailed",
						Message:            "Failed to load template './' from source 'GitRepository/mock-namespace/mock-cfn-template-git-repo'",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.TemplatePath = "./"
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			runReconciliationLoopTestCase(t, tc)
		})
	}
}

func TestCfnController_ReconcileChangeSet(t *testing.T) {
	testCases := map[string]*reconciliationLoopTestCase{
		"mark the stack as ready if the change set is empty": {
			wantedRequeueDelay: mockIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId2,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision2,
				LastAppliedRevision:    mockSourceRevision2,
				LastAttemptedChangeSet: mockChangeSetArnNewSourceRevisionAndNewGeneration,
				LastAppliedChangeSet:   mockChangeSetArnNewSourceRevisionAndNewGeneration,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "True",
						ObservedGeneration: mockGenerationId2,
						Reason:             "Succeeded",
						Message:            "Stack reconciliation succeeded",
					},
				},
			},
			fillInSource: generateMockGitRepoSource2,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId2
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId2,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision2,
					LastAppliedRevision:    mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArnNewSourceRevisionAndNewGeneration,
					LastAppliedChangeSet:   mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "Unknown",
							ObservedGeneration: mockGenerationId2,
							Reason:             "Hello",
							Message:            "World",
						},
					},
				}
			},
			markStackAsInProgress: false,
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId2, mockSourceRevision2, mockChangeSetArnNewSourceRevisionAndNewGeneration)
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:         aws.String(mockRealStackName),
					StackStatus:       sdktypes.StackStatusCreateComplete,
					StackStatusReason: aws.String("hello world"),
				}, nil)

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId2, mockSourceRevision2, mockChangeSetArnNewSourceRevisionAndNewGeneration)
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetEmpty{})

				cfnClient.EXPECT().DeleteChangeSet(expectedDescribeChangeSetIn).Return(nil)
			},
		},
		"mark the stack as ready if the change set successfully executed": {
			wantedRequeueDelay: mockIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId2,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision2,
				LastAppliedRevision:    mockSourceRevision2,
				LastAttemptedChangeSet: mockChangeSetArnNewSourceRevisionAndNewGeneration,
				LastAppliedChangeSet:   mockChangeSetArnNewSourceRevisionAndNewGeneration,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "True",
						ObservedGeneration: mockGenerationId2,
						Reason:             "Succeeded",
						Message:            "Stack reconciliation succeeded",
					},
				},
			},
			fillInSource: generateMockGitRepoSource2,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId2
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId2,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision2,
					LastAppliedRevision:    mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArnNewSourceRevisionAndNewGeneration,
					LastAppliedChangeSet:   mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "Unknown",
							ObservedGeneration: mockGenerationId2,
							Reason:             "Hello",
							Message:            "World",
						},
					},
				}
			},
			markStackAsInProgress: false,
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId2, mockSourceRevision2, mockChangeSetArnNewSourceRevisionAndNewGeneration)
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:         aws.String(mockRealStackName),
					StackStatus:       sdktypes.StackStatusCreateComplete,
					StackStatusReason: aws.String("hello world"),
				}, nil)

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId2, mockSourceRevision2, mockChangeSetArnNewSourceRevisionAndNewGeneration)
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(&clienttypes.ChangeSetDescription{
					Arn:             mockChangeSetArnNewSourceRevisionAndNewGeneration,
					Status:          sdktypes.ChangeSetStatusCreateComplete,
					ExecutionStatus: sdktypes.ExecutionStatusExecuteComplete,
				}, nil)
			},
		},
		"execute the change set if it is successfully created": {
			wantedEvents: []*expectedEvent{{
				eventType: "Normal",
				severity:  "info",
				message:   fmt.Sprintf("Change set execution started for stack 'mock-real-stack' (change set %s)", mockChangeSetArnNewSourceRevisionAndNewGeneration),
			}},
			wantedRequeueDelay: mockPollIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId2,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision2,
				LastAppliedRevision:    mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArnNewSourceRevisionAndNewGeneration,
				LastAppliedChangeSet:   mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "Unknown",
						ObservedGeneration: mockGenerationId2,
						Reason:             "Progressing",
						Message:            fmt.Sprintf("Change set execution started for stack 'mock-real-stack' (change set %s)", mockChangeSetArnNewSourceRevisionAndNewGeneration),
					},
				},
			},
			fillInSource: generateMockGitRepoSource2,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId2
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId2,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision2,
					LastAppliedRevision:    mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArnNewSourceRevisionAndNewGeneration,
					LastAppliedChangeSet:   mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "Unknown",
							ObservedGeneration: mockGenerationId2,
							Reason:             "Hello",
							Message:            "World",
						},
					},
				}
			},
			markStackAsInProgress: false,
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId2, mockSourceRevision2, mockChangeSetArnNewSourceRevisionAndNewGeneration)
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:         aws.String(mockRealStackName),
					StackStatus:       sdktypes.StackStatusCreateComplete,
					StackStatusReason: aws.String("hello world"),
				}, nil)

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId2, mockSourceRevision2, mockChangeSetArnNewSourceRevisionAndNewGeneration)
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(&clienttypes.ChangeSetDescription{
					Arn:             mockChangeSetArnNewSourceRevisionAndNewGeneration,
					Status:          sdktypes.ChangeSetStatusCreateComplete,
					ExecutionStatus: sdktypes.ExecutionStatusAvailable,
				}, nil)

				cfnClient.EXPECT().ExecuteChangeSet(expectedDescribeChangeSetIn).Return(nil)
			},
		},
	}

	inProgressChangeSetStatuses := []changeSetStatusPair{
		{
			status:          sdktypes.ChangeSetStatusCreateInProgress,
			executionStatus: sdktypes.ExecutionStatusUnavailable,
		},
		{
			status:          sdktypes.ChangeSetStatusCreatePending,
			executionStatus: sdktypes.ExecutionStatusUnavailable,
		},
		{
			status:          sdktypes.ChangeSetStatusDeleteInProgress,
			executionStatus: sdktypes.ExecutionStatusUnavailable,
		},
		{
			status:          sdktypes.ChangeSetStatusDeletePending,
			executionStatus: sdktypes.ExecutionStatusUnavailable,
		},
		{
			status:          sdktypes.ChangeSetStatusCreateComplete,
			executionStatus: sdktypes.ExecutionStatusExecuteInProgress,
		},
		{
			status:          sdktypes.ChangeSetStatusCreateComplete,
			executionStatus: sdktypes.ExecutionStatusUnavailable,
		},
	}

	for _, changeSetStatus := range inProgressChangeSetStatuses {
		expectedChangeSetStatus := changeSetStatus
		testCases[fmt.Sprintf("set stack as in-progress if the change set has %s status and %s execution status", expectedChangeSetStatus.status, expectedChangeSetStatus.executionStatus)] =
			&reconciliationLoopTestCase{
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
							Message:            fmt.Sprintf("Change set is in progress for stack 'mock-real-stack': status '%s', execution status '%s', reason ''", expectedChangeSetStatus.status, expectedChangeSetStatus.executionStatus),
						},
					},
				},
				fillInSource: generateMockGitRepoSource,
				fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
					cfnStack.Name = mockStackName
					cfnStack.Namespace = mockNamespace
					cfnStack.Generation = mockGenerationId
					cfnStack.Spec = generateMockCfnStackSpec()
					cfnStack.Status = cfnv1.CloudFormationStackStatus{
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
								Message:            "Hello world",
							},
						},
					}
				},
				mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
					expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, mockChangeSetArn)
					cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
						StackName:         aws.String(mockRealStackName),
						StackStatus:       sdktypes.StackStatusCreateComplete,
						StackStatusReason: aws.String("hello world"),
					}, nil)

					expectedDescribeChangeSetIn := generateStackInput(mockGenerationId, mockSourceRevision, mockChangeSetArn)
					cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(&clienttypes.ChangeSetDescription{
						Arn:             mockChangeSetArn,
						Status:          expectedChangeSetStatus.status,
						ExecutionStatus: expectedChangeSetStatus.executionStatus,
					}, nil)
				},
			}
	}

	failedChangeSetStatuses := []changeSetStatusPair{
		{
			status:          sdktypes.ChangeSetStatusDeleteFailed,
			executionStatus: sdktypes.ExecutionStatusUnavailable,
		},
		{
			status:          sdktypes.ChangeSetStatusFailed,
			executionStatus: sdktypes.ExecutionStatusUnavailable,
		},
		{
			status:          sdktypes.ChangeSetStatusCreateComplete,
			executionStatus: sdktypes.ExecutionStatusExecuteFailed,
		},
		{
			status:          sdktypes.ChangeSetStatusCreateComplete,
			executionStatus: sdktypes.ExecutionStatusObsolete,
		},
	}

	for _, changeSetStatus := range failedChangeSetStatuses {
		expectedChangeSetStatus := changeSetStatus
		expectedStatusMessage := fmt.Sprintf("Change set failed for stack 'mock-real-stack': status '%s', execution status '%s', reason 'hello world'", expectedChangeSetStatus.status, expectedChangeSetStatus.executionStatus)

		testCases[fmt.Sprintf("delete the change set and mark the stack as not ready if the change set has %s status and %s execution status", expectedChangeSetStatus.status, expectedChangeSetStatus.executionStatus)] =
			&reconciliationLoopTestCase{
				wantedEvents: []*expectedEvent{{
					eventType: "Warning",
					severity:  "error",
					message:   expectedStatusMessage,
				}},
				wantedRequeueDelay: mockRetryIntervalDuration,
				wantedStackStatus: &cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "False",
							ObservedGeneration: mockGenerationId,
							Reason:             "ChangeSetFailed",
							Message:            expectedStatusMessage,
						},
					},
				},
				markStackAsInProgress: false,
				fillInSource:          generateMockGitRepoSource,
				fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
					cfnStack.Name = mockStackName
					cfnStack.Namespace = mockNamespace
					cfnStack.Generation = mockGenerationId
					cfnStack.Spec = generateMockCfnStackSpec()
					cfnStack.Status = cfnv1.CloudFormationStackStatus{
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
								Message:            "Hello world",
							},
						},
					}
				},
				mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
					expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, mockChangeSetArn)
					cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
						StackName:         aws.String(mockRealStackName),
						StackStatus:       sdktypes.StackStatusCreateComplete,
						StackStatusReason: aws.String("hello world"),
					}, nil)

					expectedDescribeChangeSetIn := generateStackInput(mockGenerationId, mockSourceRevision, mockChangeSetArn)
					cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(&clienttypes.ChangeSetDescription{
						Arn:             mockChangeSetArn,
						Status:          expectedChangeSetStatus.status,
						ExecutionStatus: expectedChangeSetStatus.executionStatus,
						StatusReason:    "hello world",
					}, nil)

					cfnClient.EXPECT().DeleteChangeSet(expectedDescribeChangeSetIn).Return(nil)
				},
			}
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			runReconciliationLoopTestCase(t, tc)
		})
	}
}

func TestCfnController_ReconcileDelete(t *testing.T) {
	deleteTimestamp := metav1.NewTime(time.Now())

	testCases := map[string]*reconciliationLoopTestCase{
		"delete the stack object if the real stack does not exist": {
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "True",
						ObservedGeneration: mockGenerationId,
						Reason:             "Succeeded",
						Message:            "Hello world",
					},
				},
			},
			removeFinalizers: true,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.ObjectMeta.DeletionTimestamp = &deleteTimestamp
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.DestroyStackOnDeletion = true
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "True",
							ObservedGeneration: mockGenerationId,
							Reason:             "Succeeded",
							Message:            "Hello world",
						},
					},
				}
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, mockChangeSetArn)
				expectedDescribeStackIn.StackConfig = nil
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(nil, &cloudformation.ErrStackNotFound{})
			},
		},
		"skip deleting the real stack if the stack does not specify destroying the stack on deletion": {
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "True",
						ObservedGeneration: mockGenerationId,
						Reason:             "Succeeded",
						Message:            "Hello world",
					},
				},
			},
			removeFinalizers: true,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.ObjectMeta.DeletionTimestamp = &deleteTimestamp
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "True",
							ObservedGeneration: mockGenerationId,
							Reason:             "Succeeded",
							Message:            "Hello world",
						},
					},
				}
			},
		},
		"skip deleting the real stack if the stack is suspended": {
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "True",
						ObservedGeneration: mockGenerationId,
						Reason:             "Succeeded",
						Message:            "Hello world",
					},
				},
			},
			removeFinalizers: true,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.ObjectMeta.DeletionTimestamp = &deleteTimestamp
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Spec.DestroyStackOnDeletion = true
				cfnStack.Spec.Suspend = true
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "True",
							ObservedGeneration: mockGenerationId,
							Reason:             "Succeeded",
							Message:            "Hello world",
						},
					},
				}
			},
		},
		"handle the stack object being not found": {
			cfnStackObjectDoesNotExist: true,
		},
	}

	// Test cases when stack is in progress
	inProgressStackStatuses := []sdktypes.StackStatus{
		sdktypes.StackStatusCreateInProgress,
		sdktypes.StackStatusDeleteInProgress,
		sdktypes.StackStatusRollbackInProgress,
		sdktypes.StackStatusUpdateCompleteCleanupInProgress,
		sdktypes.StackStatusUpdateInProgress,
		sdktypes.StackStatusUpdateRollbackCompleteCleanupInProgress,
		sdktypes.StackStatusUpdateRollbackInProgress,
		sdktypes.StackStatusImportInProgress,
		sdktypes.StackStatusImportRollbackInProgress,
	}
	for _, stackStatus := range inProgressStackStatuses {
		expectedStackStatus := stackStatus
		testCases[fmt.Sprintf("set stack as in-progress if the real stack has %s status", expectedStackStatus)] =
			&reconciliationLoopTestCase{
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
							Message:            fmt.Sprintf("Stack action is in progress for stack marked for deletion 'mock-real-stack' (status '%s'), waiting for stack action to complete", expectedStackStatus),
						},
					},
				},
				fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
					cfnStack.Name = mockStackName
					cfnStack.Namespace = mockNamespace
					cfnStack.Generation = mockGenerationId
					cfnStack.Spec = generateMockCfnStackSpec()
					cfnStack.ObjectMeta.DeletionTimestamp = &deleteTimestamp
					cfnStack.Spec.DestroyStackOnDeletion = true
					cfnStack.Status = cfnv1.CloudFormationStackStatus{
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
								Message:            "Hello world",
							},
						},
					}
				},
				mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
					expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, mockChangeSetArn)
					expectedDescribeStackIn.StackConfig = nil
					cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
						StackName:   aws.String(mockRealStackName),
						StackStatus: expectedStackStatus,
					}, nil)
				},
			}
	}

	readyForCleanupStackStatuses := []sdktypes.StackStatus{
		sdktypes.StackStatusCreateFailed,
		sdktypes.StackStatusCreateComplete,
		sdktypes.StackStatusRollbackFailed,
		sdktypes.StackStatusRollbackComplete,
		sdktypes.StackStatusDeleteFailed,
		sdktypes.StackStatusDeleteComplete,
		sdktypes.StackStatusUpdateComplete,
		sdktypes.StackStatusUpdateFailed,
		sdktypes.StackStatusUpdateRollbackFailed,
		sdktypes.StackStatusUpdateRollbackComplete,
		sdktypes.StackStatusImportComplete,
		sdktypes.StackStatusImportRollbackFailed,
		sdktypes.StackStatusImportRollbackComplete,
		sdktypes.StackStatusReviewInProgress,
	}
	for _, stackStatus := range readyForCleanupStackStatuses {
		expectedStackStatus := stackStatus
		tc := &reconciliationLoopTestCase{
			wantedRequeueDelay: mockPollIntervalDuration,
			wantedEvents: []*expectedEvent{{
				eventType: "Normal",
				severity:  "info",
				message:   "Started deletion of stack 'mock-real-stack'",
			}},
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
						Message:            "Started deletion of stack 'mock-real-stack'",
					},
				},
			},
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.ObjectMeta.DeletionTimestamp = &deleteTimestamp
				cfnStack.Spec.DestroyStackOnDeletion = true
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
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
							Message:            "Hello world",
						},
					},
				}
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, mockChangeSetArn)
				expectedDescribeStackIn.StackConfig = nil
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:   aws.String(mockRealStackName),
					StackStatus: expectedStackStatus,
				}, nil)
				cfnClient.EXPECT().DeleteStack(expectedDescribeStackIn).Return(nil)
			},
		}

		if expectedStackStatus == sdktypes.StackStatusDeleteFailed {
			tc.wantedEvents = []*expectedEvent{
				{
					eventType: "Warning",
					severity:  "error",
					message:   "Stack 'mock-real-stack' failed to delete, retrying",
				},
				{
					eventType: "Normal",
					severity:  "info",
					message:   "Started deletion of stack 'mock-real-stack'",
				},
			}
		}

		testCases[fmt.Sprintf("delete the real stack if the real stack has %s status", expectedStackStatus)] = tc
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			runReconciliationLoopTestCase(t, tc)
		})
	}
}

func TestCfnController_S3Failures(t *testing.T) {
	testCases := map[string]*reconciliationLoopTestCase{
		"mark stack as not ready if template upload to S3 fails": {
			wantedErr: errors.New("template upload failed"),
			wantedEvents: []*expectedEvent{{
				eventType: "Warning",
				severity:  "error",
				message:   "Failed to reconcile stack: template upload failed",
			}},
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "TemplateUploadFailed",
						Message:            "Failed to upload template to S3 for stack 'mock-real-stack'",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			mockS3ClientCalls: func(s3Client *clientmocks.MockS3Client) {
				s3Client.EXPECT().UploadTemplate(
					mockTemplateUploadBucket,
					"",
					gomock.Any(),
					strings.NewReader(mockTemplateSourceFileContents),
				).Return("", errors.New("template upload failed"))
			},
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(nil, &cloudformation.ErrStackNotFound{})

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetNotFound{})
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			runReconciliationLoopTestCase(t, tc)
		})
	}
}

func TestCfnController_CloudFormationFailures(t *testing.T) {
	expectedErr := &sdktypes.InvalidOperationException{Message: aws.String("hello world")}
	apiFailureEvent := &expectedEvent{
		eventType: "Warning",
		severity:  "error",
		message:   "Failed to reconcile stack: InvalidOperationException: hello world",
	}
	deleteTimestamp := metav1.NewTime(time.Now())

	testCases := map[string]*reconciliationLoopTestCase{
		"mark stack as not ready if DescribeStack fails": {
			wantedErr:          expectedErr,
			wantedEvents:       []*expectedEvent{apiFailureEvent},
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "CloudFormationApiCallFailed",
						Message:            "Failed to describe the stack 'mock-real-stack'",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(nil, expectedErr)
			},
		},
		"mark stack as not ready if DescribeStack fails during stack deletion": {
			wantedErr:          expectedErr,
			wantedEvents:       []*expectedEvent{apiFailureEvent},
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "CloudFormationApiCallFailed",
						Message:            "Failed to describe the stack 'mock-real-stack'",
					},
				},
			},
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.ObjectMeta.DeletionTimestamp = &deleteTimestamp
				cfnStack.Spec.DestroyStackOnDeletion = true
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
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
							Message:            "Hello world",
						},
					},
				}
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, mockChangeSetArn)
				expectedDescribeStackIn.StackConfig = nil
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(nil, expectedErr)
			},
		},
		"mark stack as not ready if DescribeChangeSet fails": {
			wantedErr:          expectedErr,
			wantedEvents:       []*expectedEvent{apiFailureEvent},
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "CloudFormationApiCallFailed",
						Message:            "Failed to describe a change set for stack 'mock-real-stack'",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(nil, &cloudformation.ErrStackNotFound{})

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, expectedErr)
			},
		},
		"mark stack as not ready if CreateStack fails": {
			wantedErr:          expectedErr,
			wantedEvents:       []*expectedEvent{apiFailureEvent},
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "CloudFormationApiCallFailed",
						Message:            "Failed to create a change set for stack 'mock-real-stack'",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
			},
			mockS3ClientCalls: mockS3ClientUpload,
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(nil, &cloudformation.ErrStackNotFound{})

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetNotFound{})

				expectedCreateStackIn := generateStackInputWithTemplateUrl(mockGenerationId, mockSourceRevision)
				cfnClient.EXPECT().CreateStack(expectedCreateStackIn).Return("", expectedErr)
			},
		},
		"mark stack as not ready if UpdateStack fails": {
			wantedErr:          expectedErr,
			wantedEvents:       []*expectedEvent{apiFailureEvent},
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId2,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision,
				LastAppliedRevision:    mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArn,
				LastAppliedChangeSet:   mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId2,
						Reason:             "CloudFormationApiCallFailed",
						Message:            "Failed to create a change set for stack 'mock-real-stack'",
					},
				},
			},
			fillInSource: generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId2
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision,
					LastAppliedRevision:    mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArn,
					LastAppliedChangeSet:   mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "True",
							ObservedGeneration: mockGenerationId,
							Reason:             "Hello",
							Message:            "World",
						},
					},
				}
			},
			markStackAsInProgress: true,
			mockS3ClientCalls:     mockS3ClientUpload,
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId2, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:         aws.String(mockRealStackName),
					StackStatus:       sdktypes.StackStatusCreateComplete,
					StackStatusReason: aws.String("hello world"),
				}, nil)

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId2, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetNotFound{})

				expectedUpdateStackIn := generateStackInputWithTemplateUrl(mockGenerationId2, mockSourceRevision)
				cfnClient.EXPECT().UpdateStack(expectedUpdateStackIn).Return("", expectedErr)
			},
		},
		"mark stack as not ready if ContinueStackRollback fails": {
			wantedErr:          expectedErr,
			wantedEvents:       []*expectedEvent{apiFailureEvent},
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "CloudFormationApiCallFailed",
						Message:            "Failed to continue a failed rollback for stack 'mock-real-stack'",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:   aws.String(mockRealStackName),
					StackStatus: sdktypes.StackStatusUpdateRollbackFailed,
				}, nil)

				cfnClient.EXPECT().ContinueStackRollback(expectedDescribeStackIn).Return(expectedErr)
			},
		},
		"mark stack as not ready if DeleteStack fails": {
			wantedErr:          expectedErr,
			wantedEvents:       []*expectedEvent{apiFailureEvent},
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:    mockGenerationId,
				StackName:             mockRealStackName,
				LastAttemptedRevision: mockSourceRevision,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "CloudFormationApiCallFailed",
						Message:            "Failed to delete the failed stack 'mock-real-stack'",
					},
				},
			},
			markStackAsInProgress: true,
			fillInSource:          generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, "")
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:         aws.String(mockRealStackName),
					StackStatus:       sdktypes.StackStatusCreateFailed,
					StackStatusReason: aws.String("hello world"),
				}, nil)

				cfnClient.EXPECT().DeleteStack(expectedDescribeStackIn).Return(expectedErr)
			},
		},
		"mark stack as not ready if DeleteStack fails during stack deletion": {
			wantedErr:          expectedErr,
			wantedEvents:       []*expectedEvent{apiFailureEvent},
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "CloudFormationApiCallFailed",
						Message:            "Failed to delete the stack 'mock-real-stack'",
					},
				},
			},
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.ObjectMeta.DeletionTimestamp = &deleteTimestamp
				cfnStack.Spec.DestroyStackOnDeletion = true
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "True",
							ObservedGeneration: mockGenerationId,
							Reason:             "Succeeded",
							Message:            "Hello world",
						},
					},
				}
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, mockChangeSetArn)
				expectedDescribeStackIn.StackConfig = nil
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:         aws.String(mockRealStackName),
					StackStatus:       sdktypes.StackStatusCreateComplete,
					StackStatusReason: aws.String("hello world"),
				}, nil)

				cfnClient.EXPECT().DeleteStack(expectedDescribeStackIn).Return(expectedErr)
			},
		},
		"mark stack as not ready if DeleteChangeSet fails for an empty change set": {
			wantedErr:          expectedErr,
			wantedEvents:       []*expectedEvent{apiFailureEvent},
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId2,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision2,
				LastAppliedRevision:    mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArnNewSourceRevisionAndNewGeneration,
				LastAppliedChangeSet:   mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId2,
						Reason:             "CloudFormationApiCallFailed",
						Message:            "Failed to delete an empty change set for stack 'mock-real-stack'",
					},
				},
			},
			fillInSource: generateMockGitRepoSource2,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId2
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId2,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision2,
					LastAppliedRevision:    mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArnNewSourceRevisionAndNewGeneration,
					LastAppliedChangeSet:   mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "Unknown",
							ObservedGeneration: mockGenerationId2,
							Reason:             "Hello",
							Message:            "World",
						},
					},
				}
			},
			markStackAsInProgress: false,
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId2, mockSourceRevision2, mockChangeSetArnNewSourceRevisionAndNewGeneration)
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:         aws.String(mockRealStackName),
					StackStatus:       sdktypes.StackStatusCreateComplete,
					StackStatusReason: aws.String("hello world"),
				}, nil)

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId2, mockSourceRevision2, mockChangeSetArnNewSourceRevisionAndNewGeneration)
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(nil, &cloudformation.ErrChangeSetEmpty{})

				cfnClient.EXPECT().DeleteChangeSet(expectedDescribeChangeSetIn).Return(expectedErr)
			},
		},
		"mark stack as not ready if DeleteChangeSet fails for failed change set": {
			wantedErr:          expectedErr,
			wantedEvents:       []*expectedEvent{apiFailureEvent},
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId,
						Reason:             "CloudFormationApiCallFailed",
						Message:            "Failed to delete a failed change set for stack 'mock-real-stack'",
					},
				},
			},
			markStackAsInProgress: false,
			fillInSource:          generateMockGitRepoSource,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
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
							Message:            "Hello world",
						},
					},
				}
			},
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId, mockSourceRevision, mockChangeSetArn)
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:         aws.String(mockRealStackName),
					StackStatus:       sdktypes.StackStatusCreateComplete,
					StackStatusReason: aws.String("hello world"),
				}, nil)

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId, mockSourceRevision, mockChangeSetArn)
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(&clienttypes.ChangeSetDescription{
					Arn:             mockChangeSetArn,
					Status:          sdktypes.ChangeSetStatusFailed,
					ExecutionStatus: sdktypes.ExecutionStatusUnavailable,
					StatusReason:    "hello world",
				}, nil)

				cfnClient.EXPECT().DeleteChangeSet(expectedDescribeChangeSetIn).Return(expectedErr)
			},
		},
		"mark the stack as not ready if ExecuteChangeSet fails": {
			wantedErr:          expectedErr,
			wantedEvents:       []*expectedEvent{apiFailureEvent},
			wantedRequeueDelay: mockRetryIntervalDuration,
			wantedStackStatus: &cfnv1.CloudFormationStackStatus{
				ObservedGeneration:     mockGenerationId2,
				StackName:              mockRealStackName,
				LastAttemptedRevision:  mockSourceRevision2,
				LastAppliedRevision:    mockSourceRevision,
				LastAttemptedChangeSet: mockChangeSetArnNewSourceRevisionAndNewGeneration,
				LastAppliedChangeSet:   mockChangeSetArn,
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             "False",
						ObservedGeneration: mockGenerationId2,
						Reason:             "CloudFormationApiCallFailed",
						Message:            "Failed to execute a change set for stack 'mock-real-stack'",
					},
				},
			},
			fillInSource: generateMockGitRepoSource2,
			fillInInitialCfnStack: func(cfnStack *cfnv1.CloudFormationStack) {
				cfnStack.Name = mockStackName
				cfnStack.Namespace = mockNamespace
				cfnStack.Generation = mockGenerationId2
				cfnStack.Spec = generateMockCfnStackSpec()
				cfnStack.Status = cfnv1.CloudFormationStackStatus{
					ObservedGeneration:     mockGenerationId2,
					StackName:              mockRealStackName,
					LastAttemptedRevision:  mockSourceRevision2,
					LastAppliedRevision:    mockSourceRevision,
					LastAttemptedChangeSet: mockChangeSetArnNewSourceRevisionAndNewGeneration,
					LastAppliedChangeSet:   mockChangeSetArn,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             "Unknown",
							ObservedGeneration: mockGenerationId2,
							Reason:             "Hello",
							Message:            "World",
						},
					},
				}
			},
			markStackAsInProgress: false,
			mockCfnClientCalls: func(cfnClient *clientmocks.MockCloudFormationClient) {
				expectedDescribeStackIn := generateStackInput(mockGenerationId2, mockSourceRevision2, mockChangeSetArnNewSourceRevisionAndNewGeneration)
				cfnClient.EXPECT().DescribeStack(expectedDescribeStackIn).Return(&clienttypes.StackDescription{
					StackName:         aws.String(mockRealStackName),
					StackStatus:       sdktypes.StackStatusCreateComplete,
					StackStatusReason: aws.String("hello world"),
				}, nil)

				expectedDescribeChangeSetIn := generateStackInput(mockGenerationId2, mockSourceRevision2, mockChangeSetArnNewSourceRevisionAndNewGeneration)
				cfnClient.EXPECT().DescribeChangeSet(expectedDescribeChangeSetIn).Return(&clienttypes.ChangeSetDescription{
					Arn:             mockChangeSetArnNewSourceRevisionAndNewGeneration,
					Status:          sdktypes.ChangeSetStatusCreateComplete,
					ExecutionStatus: sdktypes.ExecutionStatusAvailable,
				}, nil)

				cfnClient.EXPECT().ExecuteChangeSet(expectedDescribeChangeSetIn).Return(expectedErr)
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			runReconciliationLoopTestCase(t, tc)
		})
	}
}
