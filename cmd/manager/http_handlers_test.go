package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

type fakeForceReconciler struct {
	workloads int
	images    int
	err       error
}

func (f fakeForceReconciler) ForceReconcile(context.Context) (int, int, error) {
	return f.workloads, f.images, f.err
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

func TestForceReconcileHandler(t *testing.T) {
	handler := newForceReconcileHandler(testr.New(t))
	handler.SetReconciler(fakeForceReconciler{workloads: 5, images: 12})

	req := httptest.NewRequest(http.MethodPost, "/force-reconcile", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("unexpected content type: %s", got)
	}

	var resp forceReconcileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Triggered || !resp.Success || resp.Workloads != 5 || resp.Images != 12 || resp.Message != "force reconcile completed" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestForceReconcileHandlerFailure(t *testing.T) {
	handler := newForceReconcileHandler(testr.New(t))
	handler.SetReconciler(fakeForceReconciler{workloads: 1, images: 0, err: errors.New("boom")})

	req := httptest.NewRequest(http.MethodGet, "/force-reconcile", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp forceReconcileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Triggered || resp.Success || resp.Workloads != 1 || resp.Images != 0 || !strings.Contains(resp.Message, "force reconcile failed: boom") {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestForceReconcileHandlerNotReady(t *testing.T) {
	handler := newForceReconcileHandler(testr.New(t))

	req := httptest.NewRequest(http.MethodGet, "/force-reconcile", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp forceReconcileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Triggered || resp.Success || resp.Workloads != 0 || resp.Images != 0 || resp.Message != "force reconcile service not ready" {
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
