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
	AllowedNS      []string
	Target         registry.Target
	DryRun         bool
	PathMap        []util.PathMapping
	RequestTimeout time.Duration
	Keychain       authn.Keychain
}

const defaultRequestTimeout = 2 * time.Minute

// loadRuntimeConfig resolves configuration from env vars and the optional config file.
func loadRuntimeConfig(ctx context.Context, dryRunFlag bool, fileCfg config.Config, cfgFound bool) (runtimeConfig, error) {
	allowedNS := resolveAllowedNamespaces(os.Getenv("INCLUDE_NAMESPACES"), fileCfg.IncludeNamespaces)

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
			AccountID:  eAccount,
			Region:     eRegion,
			RepoPrefix: ePrefix,
			CreateRepo: eCreate,
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

	keychain := buildKeychainFromConfig(fileCfg.RegistryCredentials)

	return runtimeConfig{
		AllowedNS:      allowedNS,
		Target:         t,
		DryRun:         dryRun,
		PathMap:        fileCfg.PathMap,
		RequestTimeout: timeout,
		Keychain:       keychain,
	}, nil
}

func resolveAllowedNamespaces(envVal string, configValues []string) []string {
	if trimmed := strings.TrimSpace(envVal); trimmed != "" {
		if ns := sanitizeNamespaces(strings.Split(trimmed, ",")); len(ns) > 0 {
			return ns
		}
	}
	if ns := sanitizeNamespaces(configValues); len(ns) > 0 {
		return ns
	}
	return []string{"*"}
}

func sanitizeNamespaces(values []string) []string {
	allowed := make([]string, 0, len(values))
	for _, ns := range values {
		if trimmed := strings.TrimSpace(ns); trimmed != "" {
			allowed = append(allowed, trimmed)
		}
	}
	return allowed
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
