# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

k8s-copycat is a Go-based Kubernetes controller that mirrors container images referenced by workloads (Deployments, StatefulSets, DaemonSets, Jobs, CronJobs, Pods) into AWS ECR or Docker-compatible registries. It operates without admission webhooks or image swaps.

## Build and Test Commands

```bash
# Build
go build -o bin/k8s-copycat ./cmd/manager

# Run locally
go run ./cmd/manager

# Build container image
docker build -t k8s-copycat:dev .

# Run all tests
make test

# Run a single test
go test -v -run TestFunctionName ./path/to/package

# Lint
make lint
```

## Deploy to Local Kind Cluster

```bash
kind create cluster --name copycat
kubectl apply -f manifests/k8s.yaml
kubectl wait --for=condition=available deployment/k8s-copycat -n k8s-copycat --timeout=120s
kubectl logs deployment/k8s-copycat -n k8s-copycat
```

## Architecture

### Controller Pattern

Uses `controller-runtime` (Kubebuilder-style) with 6 reconcilers watching different resource types. The reconcilers share a base implementation (`baseReconciler`) that handles:
- Namespace/resource filtering via skip lists
- Owner reference chain resolution (e.g., ReplicaSet → Deployment)
- Cooldown management for failed mirrors

### Key Components

- **`cmd/manager/`**: Entrypoint, configuration loading, HTTP handlers (`/reset-cooldown`, `/force-reconcile`)
- **`internal/controllers/`**: Reconciler implementations - one per resource type plus `ForceReconciler` for manual full reconciliation
- **`internal/mirror/`**: Image mirroring engine (`Pusher`) and registry credential management (`keychain.go`)
- **`internal/registry/`**: Target registry implementations (ECR, Docker) behind `Target` interface
- **`internal/config/`**: Configuration structures and YAML parsing
- **`pkg/metrics/`**: Prometheus metrics for pull/push operations
- **`pkg/util/`**: Image extraction from PodSpecs, path transformation utilities

### Configuration

Controller is configured via environment variables and/or YAML config file (`/config/config.yaml` or `CONFIG_PATH` env var). Environment variables override config file values.

Key env vars: `TARGET_KIND`, `DRY_RUN`, `DRY_PULL`, `DIGEST_PULL`, `INCLUDE_NAMESPACES`, `SKIP_NAMESPACES`

### Ports

- `:8080` - Prometheus metrics and HTTP handlers
- `:8081` - Health probes (liveness/readiness)

## Commit Convention

This project uses [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<optional scope>): <short description>
```

Types: `feat`, `fix`, `docs`, `chore`, `test`, `refactor`, `ci`, `build`

Optional git hook: `ln -s ../../hack/commit-msg .git/hooks/commit-msg`

## Testing Patterns

- Uses standard `testing` package with `controller-runtime/pkg/client/fake` for Kubernetes client mocking
- Logger testing via `testr` and `funcr` from `github.com/go-logr`
- Mock implementations for remote registry operations in `internal/mirror/`

## External Resources

- Helm chart: https://github.com/matzegebbe/k8s-copycat-helm-chart
- Container image: `ghcr.io/matzegebbe/k8s-copycat:<tag>`
