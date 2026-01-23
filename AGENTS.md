# AGENTS.md

## Project overview
k8s-copycat is a Go-based Kubernetes controller that mirrors images referenced by workloads into a registry you control.

## Repository layout
- `cmd/manager/`: main entrypoint for the controller binary.
- `internal/`: internal controller logic (reconcilers, registry interactions, helpers).
- `pkg/`: shared library-style packages used across the project.
- `manifests/`: Kubernetes manifests for deploying the controller.
- `example/`: sample configuration values and manifests.
- `docs/`: design notes, configuration details, and contributor guidance.
- `hack/`: helper scripts (for example git hooks).
- Helm chart: https://github.com/matzegebbe/k8s-copycat-helm-chart

## Build and run
### Local build
```bash
go build -o bin/k8s-copycat ./cmd/manager
```

### Run locally (for development)
```bash
go run ./cmd/manager
```

### Container image build
```bash
docker build -t k8s-copycat:dev .
```

### Deploy to a local kind cluster
```bash
kind create cluster --name copycat
kubectl apply -f manifests/k8s.yaml
kubectl wait --for=condition=available deployment/k8s-copycat -n k8s-copycat --timeout=120s
kubectl logs deployment/k8s-copycat -n k8s-copycat
```

### Tests and linting
```bash
make lint
make test
```

## Notes
- The controller is configured via environment variables and/or a YAML config file; see `docs/` and `example/` for sample configuration.
- Use the manifests under `manifests/` for cluster deployments and align the image tag with the release you intend to run.
