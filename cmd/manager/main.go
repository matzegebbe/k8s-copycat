package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	uberzap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/matzegebbe/k8s-copycat/internal/controllers"
	"github.com/matzegebbe/k8s-copycat/internal/mirror"
	"github.com/matzegebbe/k8s-copycat/pkg/util"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
}

func main() {
	metricsAddr := envOrDefault("METRICS_ADDR", ":8080")
	probeAddr := ":8081"
	var enableLeaderElection bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", metricsAddr, "metrics bind address")
	flag.StringVar(&probeAddr, "health-probe-bind-address", probeAddr, "health probe bind address")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true, "enable leader election")
	var (
		dryRunFlag  bool
		dryPullFlag bool
	)
	flag.BoolVar(&dryRunFlag, "dry-run", false, "simulate image push without actually pushing")
	flag.BoolVar(&dryPullFlag, "dry-pull", false, "simulate image pull without contacting the source registry")

	fileCfg, cfgFound, err := loadConfigFile()
	if err != nil {
		logStartupError(err, "resolve configuration failed")
		os.Exit(1)
	}

	var opts zap.Options
	configureJSONLogging(&opts)
	if levelStr := strings.TrimSpace(fileCfg.LogLevel); levelStr != "" {
		lvl, parseErr := zapcore.ParseLevel(strings.ToLower(levelStr))
		if parseErr != nil {
			logStartupError(parseErr, "resolve configuration failed", "invalidLogLevel", fileCfg.LogLevel)
			os.Exit(1)
		}
		opts.Level = lvl
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	configureJSONLogging(&opts)

	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)
	ctx := context.Background()
	cfg, err := loadRuntimeConfig(ctx, dryRunFlag, dryPullFlag, fileCfg, cfgFound)
	if err != nil {
		logger.Error(err, "resolve configuration failed 🙀")
		os.Exit(1)
	}

	cooldownHTTPHandler := newCooldownHandler(logger.WithName("cooldown"))
	forceHTTPHandler := newForceReconcileHandler(logger.WithName("force-reconcile"))

	restCfg := ctrl.GetConfigOrDie()
	kubeClient, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		logger.Error(err, "create kubernetes client failed 🙀")
		os.Exit(1)
	}

	expandedNS, err := validateAndExpandNamespaces(ctx, logger.WithName("namespaces"), kubeClient, cfg.AllowedNS)
	if err != nil {
		logger.Error(err, "validate configured namespaces failed 🙀")
		os.Exit(1)
	}
	cfg.AllowedNS = expandedNS

	mgrOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: metricsAddr,
			ExtraHandlers: map[string]http.Handler{
				"/reset-cooldown":  cooldownHTTPHandler,
				"/force-reconcile": forceHTTPHandler,
			},
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "k8s-copycat.k8s-copycat",
	}
	var syncPeriodPtr *time.Duration
	if cfg.ForceResync > 0 {
		syncPeriod := cfg.ForceResync
		syncPeriodPtr = &syncPeriod
		logger.Info("configuring periodic full reconciliation", "interval", syncPeriod)
	}
	switch {
	case len(cfg.AllowedNS) == 0:
		logger.Info("no namespaces matched include configuration; controllers will not watch any namespaces")
	case len(cfg.AllowedNS) == 1 && cfg.AllowedNS[0] == "*":
		logger.Info("listing resources in all namespaces")
	default:
		logger.Info("listing resources in configured namespaces", "namespaces", cfg.AllowedNS)
	}
	cacheOpts := cache.Options{SyncPeriod: syncPeriodPtr}
	if len(cfg.AllowedNS) != 0 && (len(cfg.AllowedNS) != 1 || cfg.AllowedNS[0] != "*") {
		nsMap := make(map[string]cache.Config, len(cfg.AllowedNS))
		for _, ns := range cfg.AllowedNS {
			nsMap[ns] = cache.Config{}
		}
		cacheOpts.DefaultNamespaces = nsMap
	}
	mgrOpts.Cache = cacheOpts
	mgr, err := ctrl.NewManager(restCfg, mgrOpts)
	if err != nil {
		logger.Error(err, "unable to start copycat 🙀")
		os.Exit(1)
	}

	transformer := util.NewRepoPathTransformer(cfg.PathMap)
	pusher := mirror.NewPusher(
		cfg.Target,
		cfg.DryRun,
		cfg.DryPull,
		transformer,
		logger.WithName("mirror"),
		cfg.Keychain,
		cfg.RequestTimeout,
		cfg.FailureCooldown,
		cfg.DigestPull,
		cfg.AllowDifferentDigestRepush,
		cfg.ExcludedRegistries,
	)
	forceReconciler, err := controllers.SetupAll(mgr, pusher, cfg.AllowedNS, cfg.SkipCfg, cfg.WatchResources, cfg.MaxConcurrentReconciles, cfg.CheckNodePlatform)
	if err != nil {
		logger.Error(err, "setup controllers failed 🙀")
		os.Exit(1)
	}
	cooldownHTTPHandler.SetResetter(pusher)
	forceHTTPHandler.SetReconciler(forceReconciler)

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error(err, "healthz failed 🙀")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error(err, "readyz failed 🙀")
		os.Exit(1)
	}

	logger.Info("starting copycat 😼")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "manager exited non-zero")
		os.Exit(1)
	}
}

func configureJSONLogging(opts *zap.Options) {
	opts.Development = false
	opts.DestWriter = os.Stdout

	encoderConfig := uberzap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.RFC3339NanoTimeEncoder

	opts.Encoder = zapcore.NewJSONEncoder(encoderConfig)
	opts.TimeEncoder = zapcore.RFC3339NanoTimeEncoder
}

func logStartupError(err error, msg string, keysAndValues ...any) {
	var opts zap.Options
	configureJSONLogging(&opts)
	zap.New(zap.UseFlagOptions(&opts)).Error(err, msg, keysAndValues...)
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
