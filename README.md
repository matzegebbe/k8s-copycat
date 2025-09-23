# ⚠️ Disclaimer

This is an **absolutely experimental WIP project**.
Do **not** use it in production environments.

# k8s-copycat for Container Images

![k8s-copycat logo](k8s-copycat-logo.png)

- Watches **Deployments**, **StatefulSets**, **Jobs**, **CronJobs**, and **Pods**
- Mirrors container images to **AWS ECR** (via AWS SDK v2 + IRSA) or a **generic Docker registry**
- Optional namespace filter via `INCLUDE_NAMESPACES` (e.g., `"default,prod"` or `"*"` for all)

## Why Does This Project Exist?

In recent times, we have repeatedly run into situations where official registries:

- Serve different image versions
- Become overloaded
- Enforce strict pull limits
- Are taken down completely
- Have images deleted without notice

To ensure we always have a reliable backup of the
images running in our cluster—**without swapping them out**—this project was born.

Yes, there are pull-through proxies like Harbor or other caching options.
But the goal here is different: to maintain a **dedicated registry** that holds all the critical images we need, so we can access them instantly when it really matters.

Additionally, we explicitly want a solution **not using admission webhooks**. In our case, we always have to work with proxies through EKS/Cilium nodeport availability. See [cilium/cilium#21959](https://github.com/cilium/cilium/issues/21959) for more context.

**Inspired by:**
[estahn/k8s-image-swapper](https://github.com/estahn/k8s-image-swapper)

## Environment Variables

- `TARGET_KIND`: `ecr` (default) or `docker`
- `AWS_REGION`, `ECR_ACCOUNT_ID`, `ECR_REPO_PREFIX`, `ECR_CREATE_REPO` (for ECR)
- `TARGET_REGISTRY`, `TARGET_REPO_PREFIX`, `TARGET_USERNAME`, `TARGET_PASSWORD`, `TARGET_INSECURE` (for Docker)
- `INCLUDE_NAMESPACES`: `*` or comma-separated list (e.g., `default,prod`)
- `SKIP_NAMESPACES`: comma-separated namespaces that should be ignored entirely
- `SKIP_DEPLOYMENTS`, `SKIP_STATEFULSETS`, `SKIP_JOBS`, `SKIP_CRONJOBS`, `SKIP_PODS`: comma-separated workload names to ignore
- `REGISTRY_REQUEST_TIMEOUT`: override the timeout for individual pull/push operations (default `2m`)
- `METRICS_ADDR`: bind address for the Prometheus metrics endpoint (default `:8080`)
- Optional `pathMap` in the config file rewrites repository paths before pushing

### Repository prefix templating

When a `repoPrefix` is configured (via config file or the corresponding
environment variables), the value can include placeholders that are replaced at
runtime. The following tokens are supported:

- `$namespace` — Namespace of the workload or pod referencing the image
- `$podname` — Name of the owning resource (or pod when available)
- `$container_name` — Name of the container that uses the image

For example, setting `repoPrefix: "$namespace/$podname"` ensures that the
resulting target repositories are unique across namespaces, even when multiple
workloads reference the same source image.

### Applying an ECR lifecycle policy

You can provide an [ECR lifecycle policy](https://docs.aws.amazon.com/AmazonECR/latest/userguide/lifecycle_policy_examples.html)
in the config file. When a repository is created by k8s-copycat, the policy is applied automatically.

```yaml
ecr:
  lifecyclePolicy: |
    {
      "rules": [
        {
          "rulePriority": 1,
          "description": "Retain only the five most recent images",
          "selection": {
            "tagStatus": "any",
            "countType": "imageCountMoreThan",
            "countNumber": 5
          },
          "action": { "type": "expire" }
        }
      ]
    }
```

### Example `config.yaml` Snippet

```yaml
pathMap:
  - from: "group/project"
    to: "prod/project"
  - from: "^legacy/(.*)"
    to: "modern/$1"
    regex: true
skipNamespaces: ["kube-system"]
skipNames:
  deployments: ["copycat"]
  cronJobs: ["nightly"]
```

Rules are evaluated in order, with the first matching entry applied. Leaving
`pathMap` empty keeps repository paths unchanged.

### Configuring registry credentials

You can provide additional credentials used when pulling source images. This is
useful for authenticating against Docker Hub or other registries even when
mirroring into a different target such as ECR.

```yaml
requestTimeout: 2m
registryCredentials:
  - registry: registry-1.docker.io
    usernameEnv: DOCKERHUB_USERNAME
    passwordEnv: DOCKERHUB_PASSWORD
  - registry: ghcr.io
    tokenEnv: GHCR_TOKEN
```

Credentials can be supplied directly in the configuration file via `username`,
`password`, or `token`, but using environment variables (referenced through
`*Env` fields) is recommended for secrets. When a token is provided it is sent as
an authentication bearer token; otherwise basic authentication is used.

## Build Container

```bash
docker build -t ghcr.io/matzegebbe/k8s-copycat:main .
```

## How It Works

- Manager (controller-runtime) runs controllers for Deployments, StatefulSets, Jobs, CronJobs, and Pods
- On events, we collect images from the PodSpec and push them to the target registry

## Dry Run Mode

You can run k8s-copycat in dry run mode to simulate image pushes without actually pushing them. This is useful for testing and validation.

Enable dry run mode by either:

- Passing the `--dry-run` flag to the binary
- Setting `dryRun: true` in your config file

## Metrics

k8s-copycat exposes Prometheus metrics on `/metrics`. The listener binds to the
address configured via `METRICS_ADDR` (default `:8080`).

### Scraping with Prometheus

Add a scrape job to your Prometheus configuration:

```yaml
scrape_configs:
  - job_name: "k8s-copycat"
    static_configs:
      - targets: ["k8s-copycat.default.svc:8080"]
```

The service publishes the following counters labelled by the image name:

- `app_registry_pull_success_total{image="<name>"}`
- `app_registry_push_success_total{image="<name>"}`

### Example Queries

```promql
sum by (image) (rate(app_registry_pull_success_total[5m]))
```

```promql
sum(rate(app_registry_push_success_total[5m]))
```

These queries reveal the busiest images and the overall push throughput.

## Contributing

See [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md) for the coding standards,
linters, and Conventional Commits policy.
