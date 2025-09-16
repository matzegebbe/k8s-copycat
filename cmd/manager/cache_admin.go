package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/matzegebbe/k8s-copycat/internal/mirror"
)

type cacheAdmin interface {
	ResetCache() []string
	Evict(target string) bool
	EvictPrefix(prefix string) []string
	CacheEntries() []mirror.CacheEntry
}

func registerCacheAdminEndpoints(mgr ctrl.Manager, p mirror.Pusher) error {
	admin, ok := p.(cacheAdmin)
	if !ok {
		return nil
	}
	log := ctrl.Log.WithName("cache-admin")

	if err := mgr.AddMetricsServerExtraHandler("/admin/cache", newCacheStateHandler(admin)); err != nil {
		return fmt.Errorf("register cache state handler: %w", err)
	}
	if err := mgr.AddMetricsServerExtraHandler("/admin/cache/evict", newCacheEvictHandler(admin, log)); err != nil {
		return fmt.Errorf("register cache eviction handler: %w", err)
	}
	return nil
}

type evictionRequest struct {
	Target string `json:"target"`
	Prefix string `json:"prefix"`
	All    bool   `json:"all"`
}

type evictionResponse struct {
	Removed   []string            `json:"removed"`
	Remaining int                 `json:"remaining"`
	Entries   []mirror.CacheEntry `json:"entries"`
}

func newCacheEvictHandler(admin cacheAdmin, log logr.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		req, err := parseEvictionRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var removed []string
		switch {
		case req.Target != "":
			if admin.Evict(req.Target) {
				removed = []string{req.Target}
			}
		case req.Prefix != "":
			removed = admin.EvictPrefix(req.Prefix)
		case req.All:
			removed = admin.ResetCache()
		default:
			// Default to evict everything when no selector is provided.
			removed = admin.ResetCache()
		}

		entries := admin.CacheEntries()
		if len(removed) > 0 {
			log.Info("evicted push cache entries", "removed", removed, "remaining", len(entries))
		}

		respondJSON(w, http.StatusOK, evictionResponse{
			Removed:   removed,
			Remaining: len(entries),
			Entries:   entries,
		})
	})
}

func newCacheStateHandler(admin cacheAdmin) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		entries := admin.CacheEntries()
		respondJSON(w, http.StatusOK, struct {
			Entries []mirror.CacheEntry `json:"entries"`
			Count   int                 `json:"count"`
		}{
			Entries: entries,
			Count:   len(entries),
		})
	})
}

func parseEvictionRequest(r *http.Request) (evictionRequest, error) {
	var req evictionRequest
	if r.Body != nil {
		defer r.Body.Close()
		if r.ContentLength != 0 {
			dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
				return req, fmt.Errorf("decode request body: %w", err)
			}
		}
	}

	q := r.URL.Query()
	if target := strings.TrimSpace(q.Get("target")); target != "" {
		req.Target = target
	}
	if prefix := strings.TrimSpace(q.Get("prefix")); prefix != "" {
		req.Prefix = prefix
	}
	if all := strings.TrimSpace(q.Get("all")); all != "" {
		req.All = parseBool(all)
	}

	req.Target = strings.TrimSpace(req.Target)
	req.Prefix = strings.TrimSpace(req.Prefix)

	if req.Target != "" && req.Prefix != "" {
		return req, fmt.Errorf("specify either target or prefix, not both")
	}
	if req.Target == "" && req.Prefix == "" && !req.All {
		req.All = true
	}

	return req, nil
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func respondJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// We cannot write an additional error at this point, just log to stderr.
		fmt.Printf("failed to encode JSON response: %v\n", err)
	}
}
