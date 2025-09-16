# ⚠️ Disclaimer

This is an **absolutely experimental WIP project**.
Do **not** use it in production environments.

# k8s-copycat for Container Images

![k8s-copycat logo](k8s-copycat-logo.png)

- Watches **Deployments**, **StatefulSets**, **Jobs**, **CronJobs**, and **Pods**
- Mirrors container images to **AWS ECR** (via AWS SDK v2 + IRSA) or a **generic Docker registry**
- Optional namespace filter via `includeNamespaces` in the config file or `INCLUDE_NAMESPACES` env (e.g., `"default,prod"` or `"*"` for all)
- Optional push throttling via `pushInterval` in the config file or `PUSH_INTERVAL` env (e.g., `1m`)

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
- `PUSH_INTERVAL`/`PUSH_INTERVALL`: throttle pushes per image (e.g., `1m`, `30s`)
- `DEBUG`: emit debug-level controller logs (e.g., `true` to surface "saw" messages)
- `LOG_LEVEL`: override controller-runtime log level (e.g., `debug`, `info`, `warn`, or numeric verbosity like `2`)
- Optional `pathMap` in the config file rewrites repository paths before pushing

### Example `config.yaml` Snippet

```yaml
debug: true
logLevel: debug
includeNamespaces:
  - default
  - prod
pushInterval: 1m
pathMap:
  - from: "group/project"
    to: "prod/project"
  - from: "^legacy/(.*)"
    to: "modern/$1"
    regex: true
```

Rules are evaluated in order, with the first matching entry applied. Leaving
`pathMap` empty keeps repository paths unchanged.

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

### Evicting the Push Cache

k8s-copycat keeps track of recently mirrored digests to avoid repeated pushes. When
running in `dryRun` mode, the digest is no longer cached so you can flip the flag
off without restarting the controller. If you were running an older version (or
just want to force a re-push), you can clear the in-memory cache via the metrics
server:

- Inspect cached entries: `curl http://localhost:8080/admin/cache`
- Evict the entire cache: `curl -X POST http://localhost:8080/admin/cache/evict`
- Remove entries by target prefix: `curl -X POST "http://localhost:8080/admin/cache/evict?prefix=123456789012.dkr.ecr.us-east-1.amazonaws.com/my-repo"`

The eviction endpoint also accepts a JSON body with `target`, `prefix`, or
`all` to control what is removed. The remaining cache contents are returned as
JSON so you can verify the effect of the command.
