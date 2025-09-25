package main

import (
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
