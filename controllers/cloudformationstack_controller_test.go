// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package controllers

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	cfnv1 "github.com/awslabs/aws-cloudformation-controller-for-flux/api/v1alpha1"
	clientmocks "github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/mocks"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/mocks"
	"github.com/fluxcd/pkg/runtime/metrics"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

const (
	mockStackName = "mock-stack"
	mockNamespace = "mock-namespace"
)

var (
	scheme = runtime.NewScheme()

	mockStackNamespacedName = types.NamespacedName{
		Name:      mockStackName,
		Namespace: mockNamespace,
	}
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
			// TODO fill in the spec for the object
			cfnStack.Name = mockStackName
			cfnStack.Namespace = mockNamespace
			// TODO fill in the status for the object
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
