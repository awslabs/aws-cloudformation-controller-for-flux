// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package main

import (
	"fmt"
	"os"
	"time"

	flag "github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/config"

	"github.com/fluxcd/pkg/runtime/acl"
	"github.com/fluxcd/pkg/runtime/client"
	helper "github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/events"
	"github.com/fluxcd/pkg/runtime/leaderelection"
	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/fluxcd/pkg/runtime/metrics"
	"github.com/fluxcd/pkg/runtime/pprof"
	"github.com/fluxcd/pkg/runtime/probes"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	"github.com/awslabs/aws-cloudformation-controller-for-flux/api/v1alpha1"
	cfnv1 "github.com/awslabs/aws-cloudformation-controller-for-flux/api/v1alpha1"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/cloudformation"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/s3"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/controllers"
	// +kubebuilder:scaffold:imports
)

const controllerName = "cfn-flux-controller"

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

var (
	// BuildSHA is the controller version
	BuildSHA string

	// BuildVersion is the controller build version
	BuildVersion string
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))
	utilruntime.Must(cfnv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr             string
		eventsAddr              string
		healthAddr              string
		concurrent              int
		requeueDependency       time.Duration
		gracefulShutdownTimeout time.Duration
		clientOptions           client.Options
		logOptions              logger.Options
		aclOptions              acl.Options
		leaderElectionOptions   leaderelection.Options
		watchOptions            helper.WatchOptions
		httpRetry               int
		awsRegion               string
		templateBucket          string
		stackTags               map[string]string
	)

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&eventsAddr, "events-addr", "", "The address of the events receiver.")
	flag.StringVar(&healthAddr, "health-addr", ":9440", "The address the health endpoint binds to.")
	flag.IntVar(&concurrent, "concurrent", 4, "The number of concurrent CloudFormationStack reconciles.")
	flag.DurationVar(&requeueDependency, "requeue-dependency", 30*time.Second, "The interval at which failing dependencies are reevaluated.")
	flag.DurationVar(&gracefulShutdownTimeout, "graceful-shutdown-timeout", 600*time.Second,
		"The duration given to the reconciler to finish before forcibly stopping.")
	flag.IntVar(&httpRetry, "http-retry", 9, "The maximum number of retries when failing to fetch artifacts over HTTP.")
	flag.StringVar(&awsRegion, "aws-region", "",
		"The AWS region where CloudFormation stacks should be deployed. Will default to the AWS_REGION environment variable.")
	flag.StringVar(&templateBucket, "template-bucket", "",
		"The S3 bucket where the controller should upload CloudFormation templates for deployment. Will default to the TEMPLATE_BUCKET environment variable.")
	flag.StringToStringVar(&stackTags, "stack-tags", map[string]string{},
		"Tag key and value pairs to apply to all CloudFormation stacks, in addition to the default tags added by the controller "+
			"(cfn-flux-controller/version, cfn-flux-controller/name, cfn-flux-controller/namespace). "+
			"Example: default-tag-name=default-tag-value,another-tag-name=another-tag-value.")

	clientOptions.BindFlags(flag.CommandLine)
	logOptions.BindFlags(flag.CommandLine)
	aclOptions.BindFlags(flag.CommandLine)
	leaderElectionOptions.BindFlags(flag.CommandLine)
	watchOptions.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(logger.NewLogger(logOptions))
	setupLog.Info("Configuring manager", "version", BuildVersion, "sha", BuildSHA)

	watchNamespace := ""
	if !watchOptions.AllNamespaces {
		watchNamespace = os.Getenv("RUNTIME_NAMESPACE")
	}

	watchSelector, err := helper.GetWatchSelector(watchOptions)
	if err != nil {
		setupLog.Error(err, "unable to configure watch label selector for manager")
		os.Exit(1)
	}

	leaderElectionId := fmt.Sprintf("%s-%s", controllerName, "leader-election")
	if watchOptions.LabelSelector != "" {
		leaderElectionId = leaderelection.GenerateID(leaderElectionId, watchOptions.LabelSelector)
	}

	restConfig := client.GetConfigOrDie(clientOptions)
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                        scheme,
		MetricsBindAddress:            metricsAddr,
		HealthProbeBindAddress:        healthAddr,
		LeaderElection:                leaderElectionOptions.Enable,
		LeaderElectionReleaseOnCancel: leaderElectionOptions.ReleaseOnCancel,
		LeaseDuration:                 &leaderElectionOptions.LeaseDuration,
		RenewDeadline:                 &leaderElectionOptions.RenewDeadline,
		RetryPeriod:                   &leaderElectionOptions.RetryPeriod,
		GracefulShutdownTimeout:       &gracefulShutdownTimeout,
		LeaderElectionID:              leaderElectionId,
		Logger:                        ctrl.Log,
		Client: ctrlclient.Options{
			Cache: &ctrlclient.CacheOptions{},
		},
		Cache: ctrlcache.Options{
			ByObject: map[ctrlclient.Object]ctrlcache.ByObject{
				&v1alpha1.CloudFormationStack{}: {Label: watchSelector},
			},
			Namespaces: []string{watchNamespace},
		},
		Controller: ctrlcfg.Controller{
			RecoverPanic:            pointer.Bool(true),
			MaxConcurrentReconciles: concurrent,
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	probes.SetupChecks(mgr, setupLog)
	pprof.SetupHandlers(mgr, setupLog)

	metricsH := helper.NewMetrics(mgr, metrics.MustMakeRecorder(), v1alpha1.CloudFormationStackFinalizer)
	var eventRecorder *events.Recorder
	if eventRecorder, err = events.NewRecorder(mgr, ctrl.Log, eventsAddr, controllerName); err != nil {
		setupLog.Error(err, "unable to create event recorder")
		os.Exit(1)
	}

	signalHandlerContext := ctrl.SetupSignalHandler()

	cfnClient, err := cloudformation.New(signalHandlerContext, awsRegion)
	if err != nil {
		setupLog.Error(err, "unable to create CloudFormation client")
		os.Exit(1)
	}

	s3Client, err := s3.New(signalHandlerContext, awsRegion)
	if err != nil {
		setupLog.Error(err, "unable to create S3 client")
		os.Exit(1)
	}

	if templateBucket == "" {
		templateBucket = os.Getenv("TEMPLATE_BUCKET")
	}

	controllerVersion := BuildVersion
	if controllerVersion == "" {
		controllerVersion = "unknown-version"
	}

	reconciler := &controllers.CloudFormationStackReconciler{
		Client:              mgr.GetClient(),
		Scheme:              mgr.GetScheme(),
		EventRecorder:       eventRecorder,
		Metrics:             metricsH,
		NoCrossNamespaceRef: aclOptions.NoCrossNamespaceRefs,
		CfnClient:           cfnClient,
		S3Client:            s3Client,
		TemplateBucket:      templateBucket,
		StackTags:           stackTags,
		ControllerName:      controllerName,
		ControllerVersion:   controllerVersion,
	}

	reconcilerOpts := controllers.CloudFormationStackReconcilerOptions{
		HTTPRetry:                 httpRetry,
		DependencyRequeueInterval: requeueDependency,
	}

	if err = reconciler.SetupWithManager(signalHandlerContext, mgr, reconcilerOpts); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", cfnv1.CloudFormationStackKind)
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	setupLog.Info("Starting manager", "version", BuildVersion, "sha", BuildSHA)

	if err := mgr.Start(signalHandlerContext); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
