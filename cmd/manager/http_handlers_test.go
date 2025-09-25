package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr/testr"
)

type fakeResetter struct {
	cleared int
	enabled bool
}

func (f fakeResetter) ResetCooldown() (int, bool) {
	return f.cleared, f.enabled
}

func TestCooldownResetHandler(t *testing.T) {
	handler := newCooldownHandler(testr.New(t))
	handler.SetResetter(fakeResetter{cleared: 3, enabled: true})

	req := httptest.NewRequest(http.MethodPost, "/reset-cooldown", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("unexpected content type: %s", got)
	}

	var resp cooldownResetResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Reset || resp.ClearedTargets != 3 || resp.Message != "failure cooldown reset" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestCooldownResetHandlerDisabled(t *testing.T) {
	handler := newCooldownHandler(testr.New(t))
	handler.SetResetter(fakeResetter{cleared: 0, enabled: false})

	req := httptest.NewRequest(http.MethodGet, "/reset-cooldown", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp cooldownResetResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Reset || resp.ClearedTargets != 0 || resp.Message != "failure cooldown disabled" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestCooldownResetHandlerNotReady(t *testing.T) {
	handler := newCooldownHandler(testr.New(t))

	req := httptest.NewRequest(http.MethodGet, "/reset-cooldown", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp cooldownResetResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Reset || resp.ClearedTargets != 0 || resp.Message != "cooldown reset service not ready" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
