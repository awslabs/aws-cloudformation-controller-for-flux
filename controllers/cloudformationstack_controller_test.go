// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package controllers

import (
	"context"
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
	// +kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))
	utilruntime.Must(cfnv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func TestCfnController_Draft(t *testing.T) {
	t.Run("should return the overriden Template", func(t *testing.T) {
		// GIVEN
		mockCtrl, ctx := gomock.WithContext(context.Background(), t)

		cfnClient := clientmocks.NewMockCloudFormationClient(mockCtrl)
		s3Client := clientmocks.NewMockS3Client(mockCtrl)
		k8sClient := mocks.NewMockClient(mockCtrl)
		eventRecorder := mocks.NewMockEventRecorder(mockCtrl)
		metricsRecorder := metrics.NewRecorder()

		reconciler := &CloudFormationStackReconciler{
			Scheme:          scheme,
			Client:          k8sClient,
			CfnClient:       cfnClient,
			S3Client:        s3Client,
			EventRecorder:   eventRecorder,
			MetricsRecorder: metricsRecorder,
		}

		request := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      "mock",
				Namespace: "mock",
			},
		}

		// WHEN
		result, err := reconciler.Reconcile(ctx, request)

		// THEN
		require.NoError(t, err)
		require.True(t, result.Requeue)
		expectedRequeueDelay, _ := time.ParseDuration("5s")
		require.Equal(t, expectedRequeueDelay, result.RequeueAfter)
	})
}
