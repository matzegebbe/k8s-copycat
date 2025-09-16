package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/matzegebbe/k8s-copycat/internal/config"
	"github.com/matzegebbe/k8s-copycat/internal/registry"
	"github.com/matzegebbe/k8s-copycat/pkg/util"
)

// runtimeConfig holds all runtime configuration derived from flags, env vars and the config file.
type runtimeConfig struct {
	AllowedNS    []string
	Target       registry.Target
	DryRun       bool
	Offline      bool
	Debug        bool
	PathMap      []util.PathMapping
	PushInterval time.Duration
	LogLevel     zapcore.LevelEnabler
}

var logLevelStrings = map[string]zapcore.Level{
	"debug":   zapcore.DebugLevel,
	"info":    zapcore.InfoLevel,
	"warn":    zapcore.WarnLevel,
	"warning": zapcore.WarnLevel,
	"error":   zapcore.ErrorLevel,
	"panic":   zapcore.PanicLevel,
}

func parseLogLevel(value string) (zapcore.LevelEnabler, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	if lvl, ok := logLevelStrings[strings.ToLower(trimmed)]; ok {
		level := zap.NewAtomicLevelAt(lvl)
		return &level, nil
	}
	if n, err := strconv.Atoi(trimmed); err == nil {
		if n <= 0 {
			return nil, fmt.Errorf("invalid log level %q", value)
		}
		level := zap.NewAtomicLevelAt(zapcore.Level(int8(-1 * n)))
		return &level, nil
	}
	return nil, fmt.Errorf("invalid log level %q", value)
}

// loadRuntimeConfig resolves configuration from env vars and the optional config file.
func loadRuntimeConfig(ctx context.Context, dryRunFlag, offlineFlag bool) (runtimeConfig, error) {
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = config.FilePath
	}
	fileCfg, ok, err := config.Load(cfgPath)
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("load config file: %w", err)
	}

	parseNamespaces := func(values []string) []string {
		out := make([]string, 0, len(values))
		for _, ns := range values {
			if trimmed := strings.TrimSpace(ns); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	}

	allowedNS := parseNamespaces(strings.Split(os.Getenv("INCLUDE_NAMESPACES"), ","))
	if len(allowedNS) == 0 {
		allowedNS = parseNamespaces(fileCfg.IncludeNamespaces)
	}
	if len(allowedNS) == 0 {
		allowedNS = []string{"*"}
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

	debug := fileCfg.Debug
	if debugEnv := os.Getenv("DEBUG"); debugEnv != "" {
		parsed, err := strconv.ParseBool(strings.TrimSpace(debugEnv))
		if err != nil {
			return runtimeConfig{}, fmt.Errorf("parse DEBUG: %w", err)
		}
		debug = parsed
	}

	intervalEnv := os.Getenv("PUSH_INTERVAL")
	if intervalEnv == "" {
		intervalEnv = os.Getenv("PUSH_INTERVALL")
	}
	var pushInterval time.Duration
	if intervalEnv != "" {
		val := strings.TrimSpace(intervalEnv)
		dur, err := time.ParseDuration(val)
		if err != nil {
			return runtimeConfig{}, fmt.Errorf("parse PUSH_INTERVAL: %w", err)
		}
		if dur < 0 {
			return runtimeConfig{}, fmt.Errorf("PUSH_INTERVAL must be >= 0, got %s", val)
		}
		pushInterval = dur
	} else if ok && fileCfg.PushInterval > 0 {
		pushInterval = fileCfg.PushInterval
	}

	var logLevel zapcore.LevelEnabler
	if lvlEnv := os.Getenv("LOG_LEVEL"); lvlEnv != "" {
		lvl, err := parseLogLevel(lvlEnv)
		if err != nil {
			return runtimeConfig{}, fmt.Errorf("parse LOG_LEVEL: %w", err)
		}
		logLevel = lvl
	} else if ok {
		lvl, err := parseLogLevel(fileCfg.LogLevel)
		if err != nil {
			return runtimeConfig{}, fmt.Errorf("parse config logLevel: %w", err)
		}
		logLevel = lvl
	}

	if logLevel == nil && debug {
		level := zap.NewAtomicLevelAt(zapcore.DebugLevel)
		logLevel = &level
	}

	return runtimeConfig{
		AllowedNS:    allowedNS,
		Target:       t,
		DryRun:       dryRun,
		Offline:      offline,
		Debug:        debug,
		PathMap:      fileCfg.PathMap,
		PushInterval: pushInterval,
		LogLevel:     logLevel,
	}, nil
}
