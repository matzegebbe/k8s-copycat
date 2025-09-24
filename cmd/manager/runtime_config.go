package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/matzegebbe/k8s-copycat/internal/config"
	"github.com/matzegebbe/k8s-copycat/internal/controllers"
	"github.com/matzegebbe/k8s-copycat/internal/mirror"
	"github.com/matzegebbe/k8s-copycat/internal/registry"
	"github.com/matzegebbe/k8s-copycat/pkg/util"
)

func loadConfigFile() (config.Config, bool, error) {
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = config.FilePath
	}
	cfg, ok, err := config.Load(cfgPath)
	if err != nil {
		return config.Config{}, false, fmt.Errorf("load config file: %w", err)
	}
	return cfg, ok, nil
}

// runtimeConfig holds all runtime configuration derived from flags, env vars and the config file.
type runtimeConfig struct {
	AllowedNS       []string
	SkipCfg         controllers.SkipConfig
	Target          registry.Target
	DryRun          bool
	PathMap         []util.PathMapping
	RequestTimeout  time.Duration
	Keychain        authn.Keychain
	FailureCooldown time.Duration
}

const defaultRequestTimeout = 2 * time.Minute

// loadRuntimeConfig resolves configuration from env vars and the optional config file.
func loadRuntimeConfig(ctx context.Context, dryRunFlag bool, fileCfg config.Config, cfgFound bool) (runtimeConfig, error) {
	allowedNS := resolveAllowedNamespaces(os.Getenv("INCLUDE_NAMESPACES"), fileCfg.IncludeNamespaces)
	skipCfg := controllers.SkipConfig{
		Namespaces:   resolveList(os.Getenv("SKIP_NAMESPACES"), fileCfg.SkipNamespaces),
		Deployments:  resolveList(os.Getenv("SKIP_DEPLOYMENTS"), fileCfg.SkipNames.Deployments),
		StatefulSets: resolveList(os.Getenv("SKIP_STATEFULSETS"), fileCfg.SkipNames.StatefulSets),
		Jobs:         resolveList(os.Getenv("SKIP_JOBS"), fileCfg.SkipNames.Jobs),
		CronJobs:     resolveList(os.Getenv("SKIP_CRONJOBS"), fileCfg.SkipNames.CronJobs),
		Pods:         resolveList(os.Getenv("SKIP_PODS"), fileCfg.SkipNames.Pods),
	}

	targetKind := os.Getenv("TARGET_KIND")
	if targetKind == "" && cfgFound {
		targetKind = strings.ToLower(strings.TrimSpace(fileCfg.TargetKind))
	}
	if targetKind == "" {
		targetKind = "ecr"
	}

	var (
		t   registry.Target
		err error
	)
	switch targetKind {
	case "ecr":
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
			AccountID:       eAccount,
			Region:          eRegion,
			RepoPrefix:      ePrefix,
			CreateRepo:      eCreate,
			LifecyclePolicy: fileCfg.ECR.LifecyclePolicy,
		}
		if cfg.AccountID == "" || cfg.Region == "" {
			return runtimeConfig{}, fmt.Errorf("for TARGET_KIND=ecr set ECR_ACCOUNT_ID and AWS_REGION (via ConfigMap or env)")
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
		dInsecure := fileCfg.Docker.Insecure
		if v := os.Getenv("TARGET_INSECURE"); v != "" {
			if parsed, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
				dInsecure = parsed
			}
		}
		d := registry.DockerConfig{
			Registry:   dRegistry,
			RepoPrefix: dPrefix,
			Username:   os.Getenv("TARGET_USERNAME"),
			Password:   os.Getenv("TARGET_PASSWORD"),
			Insecure:   dInsecure,
		}
		if d.Registry == "" {
			return runtimeConfig{}, fmt.Errorf("for TARGET_KIND=docker set TARGET_REGISTRY (via ConfigMap or env)")
		}
		t, err = registry.NewDocker(d)
	default:
		return runtimeConfig{}, fmt.Errorf("unknown TARGET_KIND %s", targetKind)
	}
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("init registry target failed: %w", err)
	}

	dryRunEnv := os.Getenv("DRY_RUN")
	dryRun := false
	if dryRunEnv != "" {
		val := strings.ToLower(strings.TrimSpace(dryRunEnv))
		if val == "1" || val == "true" || val == "yes" {
			dryRun = true
		}
	} else {
		dryRun = dryRunFlag || fileCfg.DryRun
	}

	timeoutVal := strings.TrimSpace(os.Getenv("REGISTRY_REQUEST_TIMEOUT"))
	if timeoutVal == "" {
		timeoutVal = strings.TrimSpace(fileCfg.RequestTimeout)
	}
	timeout := defaultRequestTimeout
	if timeoutVal != "" {
		parsed, parseErr := time.ParseDuration(timeoutVal)
		if parseErr != nil {
			return runtimeConfig{}, fmt.Errorf("parse registry request timeout: %w", parseErr)
		}
		timeout = parsed
	}

	cooldownMinutes := strings.TrimSpace(os.Getenv("FAILURE_COOLDOWN_MINUTES"))
	failureCooldown := mirror.DefaultFailureCooldown
	if cooldownMinutes != "" {
		minutes, parseErr := strconv.Atoi(cooldownMinutes)
		if parseErr != nil {
			return runtimeConfig{}, fmt.Errorf("parse failure cooldown minutes: %w", parseErr)
		}
		failureCooldown = cooldownFromMinutes(minutes)
	} else if fileCfg.FailureCooldownMinutes != nil {
		failureCooldown = cooldownFromMinutes(*fileCfg.FailureCooldownMinutes)
	}

	keychain := buildKeychainFromConfig(fileCfg.RegistryCredentials)

	return runtimeConfig{
		AllowedNS:       allowedNS,
		SkipCfg:         skipCfg,
		Target:          t,
		DryRun:          dryRun,
		PathMap:         fileCfg.PathMap,
		RequestTimeout:  timeout,
		Keychain:        keychain,
		FailureCooldown: failureCooldown,
	}, nil
}

func cooldownFromMinutes(minutes int) time.Duration {
	if minutes <= 0 {
		return 0
	}
	return time.Duration(minutes) * time.Minute
}

func resolveAllowedNamespaces(envVal string, configValues []string) []string {
	if ns := resolveList(envVal, configValues); len(ns) > 0 {
		return ns
	}
	return []string{"*"}
}

func resolveList(envVal string, configValues []string) []string {
	if trimmed := strings.TrimSpace(envVal); trimmed != "" {
		return sanitizeStringList(strings.Split(trimmed, ","))
	}
	return sanitizeStringList(configValues)
}

func sanitizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, val := range values {
		trimmed := strings.TrimSpace(val)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, ",") {
			parts := strings.Split(trimmed, ",")
			for _, part := range parts {
				partTrimmed := strings.TrimSpace(part)
				if partTrimmed != "" {
					out = append(out, partTrimmed)
				}
			}
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func buildKeychainFromConfig(creds []config.RegistryCredential) authn.Keychain {
	if len(creds) == 0 {
		return mirror.NewStaticKeychain(nil)
	}
	auths := make(map[string]authn.Authenticator, len(creds))
	for _, c := range creds {
		registry := strings.TrimSpace(c.Registry)
		if registry == "" {
			continue
		}
		username := strings.TrimSpace(c.Username)
		if env := strings.TrimSpace(c.UsernameEnv); env != "" {
			if val := os.Getenv(env); val != "" {
				username = val
			}
		}
		password := c.Password
		if env := strings.TrimSpace(c.PasswordEnv); env != "" {
			if val := os.Getenv(env); val != "" {
				password = val
			}
		}
		token := strings.TrimSpace(c.Token)
		if env := strings.TrimSpace(c.TokenEnv); env != "" {
			if val := os.Getenv(env); val != "" {
				token = val
			}
		}

		switch {
		case token != "":
			auths[registry] = authn.FromConfig(authn.AuthConfig{RegistryToken: token})
		case username != "" || password != "":
			auths[registry] = &authn.Basic{Username: username, Password: password}
		}
	}
	return mirror.NewStaticKeychain(auths)
}
