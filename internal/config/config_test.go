package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing.yaml")
	cfg, found, err := Load(path)
	if err != nil {
		t.Fatalf("expected missing file not to error, got %v", err)
	}
	if found {
		t.Fatalf("expected missing file to report not found")
	}
	if !reflect.DeepEqual(cfg, Config{}) {
		t.Fatalf("expected zero config for missing file, got %+v", cfg)
	}
}

func TestLoadPermissionDeniedReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission mode semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("permission denied checks are unreliable when running as root")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("targetKind: docker\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatalf("chmod config: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(path, 0o600)
	})

	_, found, err := Load(path)
	if err == nil {
		t.Fatalf("expected permission denied to return error")
	}
	if found {
		t.Fatalf("expected permission denied not to report config as found")
	}
}
