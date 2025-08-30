package main

import (
	"context"
	"flag"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/go-logr/logr"

	"github.com/matzegebbe/doppler/internal/config"
	"github.com/matzegebbe/doppler/internal/controllers"
	"github.com/matzegebbe/doppler/internal/mirror"
	"github.com/matzegebbe/doppler/internal/registry"
	"github.com/matzegebbe/doppler/pkg/util"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var enableLeaderElection bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "metrics bind address")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "health probe bind address")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true, "enable leader election")
	var dryRunFlag bool
	flag.BoolVar(&dryRunFlag, "dry-run", false, "simulate image push without actually pushing")
	opts := zap.Options{Development: false}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "doppler.k8s-image-doppler",
	})
	if err != nil {
		logger.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ctx := context.Background()

	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = config.FilePath
	}
	fileCfg, ok, err := config.Load(cfgPath)
	if err != nil {
		logger.Error(err, "failed to load config file", "path", cfgPath)
		os.Exit(1)
	}

	targetKind := os.Getenv("TARGET_KIND")
	if targetKind == "" && ok {
		targetKind = strings.ToLower(strings.TrimSpace(fileCfg.TargetKind))
	}
	if targetKind == "" {
		targetKind = "ecr"
	}

	var t registry.Target
	switch targetKind {
	case "ecr":
		// env overrides config file
		eAccount := fileCfg.ECR.AccountID
		if v := os.Getenv("ECR_ACCOUNT_ID"); v != "" {
			eAccount = v
		}
		eRegion := fileCfg.ECR.Region
		if v := os.Getenv("AWS_REGION"); v != "" {
			eRegion = v
		}
		ePrefix := fileCfg.ECR.RepoPrefix
		if v := os.Getenv("ECR_REPO_PREFIX"); v != "" {
			ePrefix = v
		}
		eCreate := true
		if fileCfg.ECR.CreateRepo != nil {
			eCreate = *fileCfg.ECR.CreateRepo
		}
		if v := os.Getenv("ECR_CREATE_REPO"); v == "false" {
			eCreate = false
		}

		cfg := registry.ECRConfig{
			AccountID:  eAccount,
			Region:     eRegion,
			RepoPrefix: ePrefix,
			CreateRepo: eCreate,
		}
		if cfg.AccountID == "" || cfg.Region == "" {
			logger.Error(nil, "for TARGET_KIND=ecr set ECR_ACCOUNT_ID and AWS_REGION (via ConfigMap or env)")
			os.Exit(1)
		}
		t, err = registry.NewECR(ctx, cfg)

	case "docker":
		dRegistry := fileCfg.Docker.Registry
		if v := os.Getenv("TARGET_REGISTRY"); v != "" {
			dRegistry = v
		}
		dPrefix := fileCfg.Docker.RepoPrefix
		if v := os.Getenv("TARGET_REPO_PREFIX"); v != "" {
			dPrefix = v
		}
		d := registry.DockerConfig{
			Registry:   dRegistry,
			RepoPrefix: dPrefix,
			Username:   os.Getenv("TARGET_USERNAME"),
			Password:   os.Getenv("TARGET_PASSWORD"),
		}
		if d.Registry == "" {
			logger.Error(nil, "for TARGET_KIND=docker set TARGET_REGISTRY (via ConfigMap or env)")
			os.Exit(1)
		}
		t, err = registry.NewDocker(d)

	default:
		logger.Error(nil, "unknown TARGET_KIND", "TARGET_KIND", targetKind)
		os.Exit(1)
	}
	if err != nil {
		logger.Error(err, "init registry target failed")
		os.Exit(1)
	}

	includeEnv := os.Getenv("INCLUDE_NAMESPACES")
	if includeEnv == "" {
		includeEnv = "*"
	}
	allowedNS := strings.Split(includeEnv, ",")

	dryRun := dryRunFlag || fileCfg.DryRun

	pusher := mirror.NewPusher(t, dryRun)
	if err := controllers.SetupAll(mgr, pusher, allowedNS); err != nil {
		logger.Error(err, "setup controllers failed")
		os.Exit(1)
	}

	// Register startup image push as a Runnable
	if err := mgr.Add(&StartupImagePush{Client: mgr.GetClient(), AllowedNS: allowedNS, Pusher: pusher, Logger: logger}); err != nil {
		logger.Error(err, "failed to add startup image push runnable")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error(err, "healthz failed")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error(err, "readyz failed")
		os.Exit(1)
	}

	logger.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "manager exited non-zero")
		os.Exit(1)
	}
}

// StartupImagePush implements Runnable for pushing images on startup
type StartupImagePush struct {
	Client    client.Client
	AllowedNS []string
	Pusher    mirror.Pusher
	Logger    logr.Logger
}

func (s *StartupImagePush) Start(ctx context.Context) error {
	for _, ns := range s.AllowedNS {
		if ns == "*" {
			var nsList corev1.NamespaceList
			if err := s.Client.List(ctx, &nsList); err != nil {
				s.Logger.Error(err, "failed to list namespaces")
				return err
			}
			for _, nsObj := range nsList.Items {
				pushImagesInNamespace(ctx, s.Client, nsObj.Name, s.Pusher, s.Logger)
			}
		} else {
			pushImagesInNamespace(ctx, s.Client, ns, s.Pusher, s.Logger)
		}
	}
	return nil
}

// Helper function to push all images in a namespace
func pushImagesInNamespace(ctx context.Context, k8sClient client.Client, namespace string, pusher mirror.Pusher, logger logr.Logger) {
	var podList corev1.PodList
	if err := k8sClient.List(ctx, &podList, client.InNamespace(namespace)); err != nil {
		logger.Error(err, "failed to list pods", "namespace", namespace)
		return
	}
	imageSet := map[string]struct{}{}
	for _, pod := range podList.Items {
		for _, img := range util.ImagesFromPodSpec(&pod.Spec) {
			imageSet[img] = struct{}{}
		}
	}
	for img := range imageSet {
		if err := pusher.Mirror(ctx, img); err != nil {
			logger.Error(err, "failed to push image", "image", img)
		}
	}
}
