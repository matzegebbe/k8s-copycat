package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/matzegebbe/doppler/internal/config"
	"github.com/matzegebbe/doppler/internal/registry"
	"github.com/matzegebbe/doppler/pkg/util"
)

// runtimeConfig holds all runtime configuration derived from flags, env vars and the config file.
type runtimeConfig struct {
	AllowedNS []string
	Target    registry.Target
	DryRun    bool
	Offline   bool
	PathMap   []util.PathMapping
}

// loadRuntimeConfig resolves configuration from env vars and the optional config file.
func loadRuntimeConfig(ctx context.Context, dryRunFlag, offlineFlag bool) (runtimeConfig, error) {
	includeEnv := os.Getenv("INCLUDE_NAMESPACES")
	if includeEnv == "" {
		includeEnv = "*"
	}
	rawNS := strings.Split(includeEnv, ",")
	allowedNS := make([]string, 0, len(rawNS))
	for _, ns := range rawNS {
		if trimmed := strings.TrimSpace(ns); trimmed != "" {
			allowedNS = append(allowedNS, trimmed)
		}
	}
	if len(allowedNS) == 0 {
		allowedNS = []string{"*"}
	}

	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = config.FilePath
	}
	fileCfg, ok, err := config.Load(cfgPath)
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("load config file: %w", err)
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

	offlineEnv := os.Getenv("OFFLINE")
	offline := false
	if offlineEnv != "" {
		val := strings.ToLower(strings.TrimSpace(offlineEnv))
		if val == "1" || val == "true" || val == "yes" {
			offline = true
		}
	} else {
		offline = offlineFlag || fileCfg.Offline
	}

	return runtimeConfig{
		AllowedNS: allowedNS,
		Target:    t,
		DryRun:    dryRun,
		Offline:   offline,
		PathMap:   fileCfg.PathMap,
	}, nil
}
