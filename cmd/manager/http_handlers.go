package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-logr/logr"
)

type cooldownResetter interface {
	ResetCooldown() (cleared int, cooldownEnabled bool)
}

type cooldownResetResponse struct {
	Reset          bool   `json:"reset"`
	ClearedTargets int    `json:"clearedTargets"`
	Message        string `json:"message"`
}

type cooldownHandler struct {
	log      logr.Logger
	mu       sync.RWMutex
	resetter cooldownResetter
}

func newCooldownHandler(log logr.Logger) *cooldownHandler {
	return &cooldownHandler{log: log}
}

func (h *cooldownHandler) SetResetter(resetter cooldownResetter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resetter = resetter
}

func (h *cooldownHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	resetter := h.resetter
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	if resetter == nil {
		resp := cooldownResetResponse{Message: "cooldown reset service not ready"}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			h.log.Error(err, "encode cooldown reset response")
		}
		return
	}

	cleared, enabled := resetter.ResetCooldown()

	response := cooldownResetResponse{
		Reset:          enabled && cleared > 0,
		ClearedTargets: cleared,
	}

	if !enabled {
		response.Message = "failure cooldown disabled"
	} else if cleared == 0 {
		response.Message = "no cooldown entries to reset"
	} else {
		response.Message = "failure cooldown reset"
	}

	h.log.Info("processed cooldown reset request", "method", r.Method, "clearedTargets", cleared, "cooldownEnabled", enabled)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.log.Error(err, "encode cooldown reset response")
	}
}

type forceReconcilable interface {
	ForceReconcile(ctx context.Context) (workloads int, images int, err error)
}

type forceReconcileResponse struct {
	Triggered bool   `json:"triggered"`
	Success   bool   `json:"success"`
	Workloads int    `json:"workloadsProcessed"`
	Images    int    `json:"imagesMirrored"`
	Message   string `json:"message"`
}

type forceReconcileHandler struct {
	log        logr.Logger
	mu         sync.RWMutex
	reconciler forceReconcilable
}

func newForceReconcileHandler(log logr.Logger) *forceReconcileHandler {
	return &forceReconcileHandler{log: log}
}

func (h *forceReconcileHandler) SetReconciler(reconciler forceReconcilable) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reconciler = reconciler
}

func (h *forceReconcileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	reconciler := h.reconciler
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	if reconciler == nil {
		resp := forceReconcileResponse{Message: "force reconcile service not ready"}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			h.log.Error(err, "encode force reconcile response")
		}
		return
	}

	workloads, images, err := reconciler.ForceReconcile(r.Context())

	response := forceReconcileResponse{
		Triggered: true,
		Workloads: workloads,
		Images:    images,
	}

	if err != nil {
		response.Success = false
		response.Message = "force reconcile failed: " + err.Error()
		h.log.Error(err, "processed force reconcile request", "method", r.Method, "workloadsProcessed", workloads, "imagesMirrored", images)
	} else {
		response.Success = true
		response.Message = "force reconcile completed"
		h.log.Info("processed force reconcile request", "method", r.Method, "workloadsProcessed", workloads, "imagesMirrored", images)
	}

	if encodeErr := json.NewEncoder(w).Encode(response); encodeErr != nil {
		h.log.Error(encodeErr, "encode force reconcile response")
	}
}
