/*
Copyright 2025.

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

package main

import (
	"crypto/tls"
	"flag"
	"os"
	"strconv"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/softcdata/testudo-operator/internal/controller"
	"github.com/softcdata/testudo-operator/internal/controller/appbackup"
	"github.com/softcdata/testudo-operator/internal/controller/apprestore"
	"github.com/softcdata/testudo-operator/internal/controller/datasync"
	"github.com/softcdata/testudo-operator/internal/controller/disasterdrill"
	"github.com/softcdata/testudo-operator/internal/controller/disastergroup"
	"github.com/softcdata/testudo-operator/internal/controller/disasterinstance"
	"github.com/softcdata/testudo-operator/internal/controller/disasteroperation"
	"github.com/softcdata/testudo-operator/internal/controller/resourcesync"
	runtimecfg "github.com/softcdata/testudo-operator/internal/controller/runtimeconfig"
	"github.com/softcdata/testudo-operator/internal/controller/scheduler"
	"github.com/softcdata/testudo-operator/internal/dependencybackfill"
	clusterwebhook "github.com/softcdata/testudo-operator/internal/webhook/cluster"
	diwebhook "github.com/softcdata/testudo-operator/internal/webhook/disasterinstance"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	platformlicense "github.com/softcdata/testudo-operator/pkg/license"
	// +kubebuilder:scaffold:imports
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(disasterv1.AddToScheme(scheme))

	// 添加 apiextensionsv1 以支持 CRD 操作（用于手动安装 Velero CRDs）
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var dependencyBackfillOnStart bool
	var managementNamespace string
	var licenseNamespace string
	var licenseCAPath string
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.BoolVar(&dependencyBackfillOnStart, "dependency-backfill-on-start", true,
		"If set, run one-time dependency label backfill for existing resources before manager start.")
	flag.StringVar(&managementNamespace, "management-namespace", controller.DefaultManagementNamespace,
		"The namespace containing Testudo management-plane namespaced resources.")
	flag.StringVar(&licenseNamespace, "license-namespace", "",
		"The namespace containing the platform license Secret and license status ConfigMaps.")
	flag.StringVar(&licenseCAPath, "license-ca-path", platformlicense.DefaultServiceAccountCAPath,
		"The API server CA bundle path used for k8s-v1 license fingerprint calculation.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	controller.SetManagementNamespace(managementNamespace)
	managementNamespace = controller.ManagementNamespace()
	if strings.TrimSpace(licenseNamespace) == "" {
		licenseNamespace = managementNamespace
	} else {
		licenseNamespace = strings.TrimSpace(licenseNamespace)
	}
	helper.SetDefaultEventNamespace(managementNamespace)

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	appRestoreRuntimeOptions := loadAppRestoreRuntimeOptions()
	runtimecfg.SetStartupDefaults(loadStartupRuntimeConfig(appRestoreRuntimeOptions))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	err := velerov1.AddToScheme(scheme)
	if err != nil {
		setupLog.Error(err, "unable to add velero to the scheme")
		os.Exit(1)
	}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "testudo-operator.softcdata.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	licenseVerifier := platformlicense.NewDefaultVerifier()
	webhookServer = mgr.GetWebhookServer()
	webhookServer.Register(
		diwebhook.ValidateDisasterInstancePath,
		&admission.Webhook{
			Handler: diwebhook.NewRestorePolicyValidator(mgr.GetClient()),
		},
	)
	webhookServer.Register(
		clusterwebhook.ValidateClusterPath,
		&admission.Webhook{
			Handler: clusterwebhook.NewValidator(mgr.GetClient(), licenseNamespace, licenseCAPath, licenseVerifier),
		},
	)

	if err := (&runtimecfg.Reconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor("runtimeconfig-controller"),
		Namespace: managementNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "OperatorRuntimeConfig")
		os.Exit(1)
	}
	if err := (&controller.DisasterBackupReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DisasterBackup")
		os.Exit(1)
	}
	if err := (&controller.ClusterReconciler{
		Client:              mgr.GetClient(),
		Scheme:              mgr.GetScheme(),
		Recorder:            mgr.GetEventRecorderFor("cluster-controller"),
		ManagementNamespace: managementNamespace,
		LicenseGateEnabled:  true,
		LicenseNamespace:    licenseNamespace,
		LicenseCAPath:       licenseCAPath,
		LicenseVerifier:     licenseVerifier,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cluster")
		os.Exit(1)
	}
	if err := (&controller.DisasterConfigReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("disasterconfig-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DisasterConfig")
		os.Exit(1)
	}
	if err := (&controller.DisasterJobReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DisasterJob")
		os.Exit(1)
	}
	if err := (&controller.BackupPolicyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BackupPolicy")
		os.Exit(1)
	}
	if err := (&controller.StorageRepositoryReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("storagerepository-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "StorageRepository")
		os.Exit(1)
	}
	if err := (&appbackup.AppBackupReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AppBackup")
		os.Exit(1)
	}
	appRestoreReconciler := apprestore.NewAppRestoreReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetEventRecorderFor("apprestore-controller"),
		apprestore.WithRestoreRuntime(appRestoreRuntimeOptions...),
	)
	if err := appRestoreReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AppRestore")
		os.Exit(1)
	}
	if err := (&controller.DisasterPolicyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DisasterPolicy")
		os.Exit(1)
	}

	// V2 Controllers - Disaster Orchestration
	// Initialize SyncScheduler for DataSync and ResourceSync
	syncScheduler, err := scheduler.NewSyncScheduler()
	if err != nil {
		setupLog.Error(err, "unable to create sync scheduler")
		os.Exit(1)
	}
	syncScheduler.Start()
	defer func() {
		if err := syncScheduler.Shutdown(); err != nil {
			setupLog.Error(err, "failed to shutdown sync scheduler")
		}
	}()
	setupLog.Info("SyncScheduler started")

	if err := (&disasterinstance.DisasterInstanceReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName("DisasterInstance"),
		Recorder: mgr.GetEventRecorderFor("disasterinstance-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DisasterInstance")
		os.Exit(1)
	}
	if err := (&datasync.DataSyncReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Log:       ctrl.Log.WithName("controllers").WithName("DataSync"),
		Recorder:  mgr.GetEventRecorderFor("datasync-controller"),
		Scheduler: syncScheduler,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DataSync")
		os.Exit(1)
	}
	if err := (&resourcesync.ResourceSyncReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Log:       ctrl.Log.WithName("controllers").WithName("ResourceSync"),
		Recorder:  mgr.GetEventRecorderFor("resourcesync-controller"),
		Scheduler: syncScheduler,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ResourceSync")
		os.Exit(1)
	}
	if err := (&disasteroperation.DisasterOperationReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName("DisasterOperation"),
		Recorder: mgr.GetEventRecorderFor("disasteroperation-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DisasterOperation")
		os.Exit(1)
	}

	if err := (&disastergroup.DisasterGroupReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName("DisasterGroup"),
		Recorder: mgr.GetEventRecorderFor("disastergroup-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DisasterGroup")
		os.Exit(1)
	}

	if err := (&disasterdrill.DisasterDrillReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName("DisasterDrill"),
		Recorder: mgr.GetEventRecorderFor("disasterdrill-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DisasterDrill")
		os.Exit(1)
	}

	managerCtx := ctrl.SetupSignalHandler()

	if dependencyBackfillOnStart {
		setupLog.Info("registering dependency label backfill startup runnable")
		backfillRunner := dependencybackfill.NewRunner(
			mgr.GetAPIReader(),
			mgr.GetClient(),
			ctrl.Log.WithName("dependency-backfill"),
		)
		backfillRunner.SetManagementNamespace(managementNamespace)
		if err := mgr.Add(dependencybackfill.NewStartupRunnable(backfillRunner, setupLog.WithName("dependency-backfill"))); err != nil {
			setupLog.Error(err, "unable to register dependency label backfill runnable")
			os.Exit(1)
		}
	}
	if err := mgr.Add(controller.NewLicenseStatusRunnable(mgr.GetClient(), licenseNamespace, licenseCAPath, licenseVerifier)); err != nil {
		setupLog.Error(err, "unable to register license status runnable")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(managerCtx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func loadAppRestoreRuntimeOptions() []apprestore.RestoreRuntimeOption {
	opts := make([]apprestore.RestoreRuntimeOption, 0, 7)

	if v, ok := parseEnvDuration("APPRESTORE_IN_PROGRESS_MAX_WAIT"); ok {
		opts = append(opts, apprestore.WithRestoreInProgressMaxWaitDefault(v))
	}
	if v, ok := parseEnvDuration("APPRESTORE_UNKNOWN_MAX_WAIT"); ok {
		opts = append(opts, apprestore.WithRestoreUnknownMaxWaitDefault(v))
	}
	if v, ok := parseEnvDuration("APPRESTORE_PROGRESS_COMPLETE_GRACE"); ok {
		opts = append(opts, apprestore.WithProgressCompleteGrace(v))
	}
	if v, ok := parseEnvDuration("APPRESTORE_STARTUP_GRACE"); ok {
		opts = append(opts, apprestore.WithStartupGrace(v))
	}
	if v, ok := parseEnvDuration("APPRESTORE_MISSING_GRACE"); ok {
		opts = append(opts, apprestore.WithMissingGrace(v))
	}
	if v, ok := parseEnvDuration("APPRESTORE_EMPTY_STATUS_GRACE"); ok {
		opts = append(opts, apprestore.WithEmptyStatusGrace(v))
	}
	if v, ok := parseEnvDuration("APPRESTORE_PVR_PENDING_MAX_WAIT"); ok {
		opts = append(opts, apprestore.WithPodVolumeRestorePendingMaxWait(v))
	}
	if v, ok := parseEnvInt("APPRESTORE_RETRY_LIMIT"); ok {
		opts = append(opts, apprestore.WithAutoRetryLimit(v))
	}
	if v, ok := parseEnvInt("APPRESTORE_RETRY_LIMIT_PROGRESS"); ok {
		opts = append(opts, apprestore.WithAutoRetryLimitProgress(v))
	}
	if v, ok := parseEnvInt("APPRESTORE_RETRY_LIMIT_STARTUP"); ok {
		opts = append(opts, apprestore.WithAutoRetryLimitStartup(v))
	}
	if v, ok := parseEnvInt("APPRESTORE_RETRY_LIMIT_MISSING"); ok {
		opts = append(opts, apprestore.WithAutoRetryLimitMissing(v))
	}
	if v, ok := parseEnvInt("APPRESTORE_RETRY_LIMIT_EMPTY"); ok {
		opts = append(opts, apprestore.WithAutoRetryLimitEmpty(v))
	}
	if v, ok := parseEnvDuration("APPRESTORE_RETRY_BACKOFF"); ok {
		opts = append(opts, apprestore.WithRetryBackoff(v))
	}

	return opts
}

func loadStartupRuntimeConfig(appRestoreRuntimeOptions []apprestore.RestoreRuntimeOption) runtimecfg.Snapshot {
	snapshot := runtimecfg.DefaultSnapshot()
	snapshot.Source = "startup"
	restoreRuntime := apprestore.NewRestoreRuntimeConfig(appRestoreRuntimeOptions...)
	snapshot.RestoreRuntime.InProgressMaxWait = restoreRuntime.RestoreInProgressMaxWaitDefault
	snapshot.RestoreRuntime.UnknownMaxWait = restoreRuntime.RestoreUnknownMaxWaitDefault
	snapshot.RestoreRuntime.InProgressPollInterval = restoreRuntime.RestoreInProgressPollInterval
	snapshot.RestoreRuntime.UnknownPollInterval = restoreRuntime.RestoreUnknownPollInterval
	snapshot.RestoreRuntime.ProgressCompleteGrace = restoreRuntime.ProgressCompleteGrace
	snapshot.RestoreRuntime.StartupGrace = restoreRuntime.StartupGrace
	snapshot.RestoreRuntime.MissingGrace = restoreRuntime.MissingGrace
	snapshot.RestoreRuntime.EmptyStatusGrace = restoreRuntime.EmptyStatusGrace
	snapshot.RestoreRuntime.PodVolumeRestorePendingWait = restoreRuntime.PodVolumeRestorePendingMaxWait
	snapshot.RestoreRuntime.RetryBackoff = restoreRuntime.RetryBackoff
	snapshot.RestoreRuntime.RetryLimit = restoreRuntime.AutoRetryLimit
	snapshot.RestoreRuntime.RetryLimitProgress = restoreRuntime.AutoRetryLimitProgress
	snapshot.RestoreRuntime.RetryLimitStartup = restoreRuntime.AutoRetryLimitStartup
	snapshot.RestoreRuntime.RetryLimitMissing = restoreRuntime.AutoRetryLimitMissing
	snapshot.RestoreRuntime.RetryLimitEmpty = restoreRuntime.AutoRetryLimitEmpty
	return snapshot
}

func parseEnvDuration(envKey string) (time.Duration, bool) {
	raw, exists := os.LookupEnv(envKey)
	if !exists || strings.TrimSpace(raw) == "" {
		return 0, false
	}
	v, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		setupLog.Info("invalid duration env, using default runtime config", "env", envKey, "value", raw, "error", err.Error())
		return 0, false
	}
	return v, true
}

func parseEnvInt(envKey string) (int, bool) {
	raw, exists := os.LookupEnv(envKey)
	if !exists || strings.TrimSpace(raw) == "" {
		return 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		setupLog.Info("invalid int env, using default runtime config", "env", envKey, "value", raw, "error", err.Error())
		return 0, false
	}
	return v, true
}
