package main

import (
	"context"
	"flag"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
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
	var metricsAddr string
	var probeAddr string
	var enableLeaderElection bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "metrics bind addres")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "health probe bind address")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true, "enable leader election")
	var dryRunFlag bool
	flag.BoolVar(&dryRunFlag, "dry-run", false, "simulate image push without actually pushing")

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
	cfg, err := loadRuntimeConfig(ctx, dryRunFlag, fileCfg, cfgFound)
	if err != nil {
		logger.Error(err, "resolve configuration failed 🙀")
		os.Exit(1)
	}

	mgrOpts := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                server.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "k8s-copycat.k8s-copycat",
	}
	if len(cfg.AllowedNS) != 1 && cfg.AllowedNS[0] != "*" {
		nsMap := make(map[string]cache.Config, len(cfg.AllowedNS))
		for _, ns := range cfg.AllowedNS {
			nsMap[ns] = cache.Config{}
		}
		mgrOpts.Cache = cache.Options{DefaultNamespaces: nsMap}
	}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOpts)
	if err != nil {
		logger.Error(err, "unable to start copycat 🙀")
		os.Exit(1)
	}

	transformer := util.NewRepoPathTransformer(cfg.PathMap)
	pusher := mirror.NewPusher(cfg.Target, cfg.DryRun, transformer, logger.WithName("mirror"), cfg.Keychain, cfg.RequestTimeout)
	if err := controllers.SetupAll(mgr, pusher, cfg.AllowedNS); err != nil {
		logger.Error(err, "setup controllers failed 🙀")
		os.Exit(1)
	}

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
