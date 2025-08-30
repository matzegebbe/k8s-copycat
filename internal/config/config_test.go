package config

import (
	"os"
	"testing"
)

func TestLoadStartupPush(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer tmp.Close()

	if _, err := tmp.WriteString("startupPush: false\n"); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	cfg, ok, err := Load(tmp.Name())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true when file exists")
	}
	if cfg.StartupPush == nil {
		t.Fatalf("StartupPush should not be nil")
	}
	if *cfg.StartupPush != false {
		t.Fatalf("StartupPush expected false got %v", *cfg.StartupPush)
	}
}

func TestLoadNoFile(t *testing.T) {
	cfg, ok, err := Load("/non/existent/path.yaml")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false when file missing")
	}
	if cfg.StartupPush != nil {
		t.Fatalf("StartupPush should be nil for missing config")
	}
}
