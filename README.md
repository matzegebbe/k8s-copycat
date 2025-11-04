# k8s-copycat

![k8s-copycat logo](k8s-copycat-logo.png)

> Continuously mirror the images your Kubernetes workloads already run into the registry you control.

## ⚠️ Disclaimer

This is an **absolutely experimental WIP project**. Do **not** use it in production environments.

## Table of Contents

- [Overview](#overview)
- [Why k8s-copycat?](#why-k8s-copycat)
- [Key Capabilities](#key-capabilities)
- [Getting Started](#getting-started)
- [Configuration](#configuration)
  - [Environment variables](#environment-variables)
  - [Digest-based mirroring](#digest-based-mirroring)
  - [Watching workloads](#watching-workloads)
  - [Repository prefix templating](#repository-prefix-templating)
  - [Lifecycle policies](#lifecycle-policies)
  - [Example configuration](#example-configuration)
  - [Registry credentials](#registry-credentials)
- [Troubleshooting mirrors](#troubleshooting-mirrors)
- [Inspiration](#inspiration)

## Overview

k8s-copycat monitors **Deployments**, **StatefulSets**, **DaemonSets**, **Jobs**, **CronJobs**, and **Pods** to mirror their container images into **AWS ECR** or any other Docker-compatible registry. It keeps your recovery registry in sync with what is actively running—no image swaps, admission webhooks, or pod restarts required.

The controller runs inside your cluster and reacts to changes in workload specs and status. Once configured, it continuously copies referenced images into a registry that you own so they are available when upstream registries throttle, disappear, or delete content without notice.

## Why k8s-copycat?

Over the last years we repeatedly encountered scenarios where official registries:

- Serve different image versions
- Become overloaded
- Enforce strict pull limits
- Are taken down completely
- Delete images without notice

To guarantee access to the exact images already running in our cluster—**without swapping them out**—we built k8s-copycat. Unlike pull-through proxies such as Harbor, copycat maintains a **dedicated mirror registry** that only receives artifacts you choose to replicate.

We also needed a solution that **does not rely on admission webhooks** because of network restrictions in EKS/Cilium environments (see [cilium/cilium#21959](https://github.com/cilium/cilium/issues/21959) for background). Copycat accomplishes this by watching workload resources directly through the Kubernetes API.

## Key Capabilities

- Continuously mirrors workloads into ECR or any Docker-compatible registry
- Supports namespace allow/deny lists, workload skip lists, and registry exclusions
- Handles manifest lists, attestations, and multi-architecture images, mirroring the platforms your workloads actually use and any extras you list in `mirrorPlatforms`
- Provides templated repository prefixes to segregate mirrored content
- Exposes Prometheus metrics for observability
- Operates in dry-run modes to validate configuration before pushing

## Getting Started

Clone the repository, build the controller, and apply the Kubernetes manifests under [`manifests/`](manifests/) tailored to your cluster. A sample configuration is available in [`example/`](example/) and the [`docs/`](docs/) directory contains deep dives into mirroring logic, deployment options, and operational guidance.

At minimum you will need to:

1. Choose a target registry (`TARGET_KIND=ecr` or `TARGET_KIND=docker`).
2. Provide credentials that allow pulling from source registries and pushing to the destination.
3. Deploy the controller with access to watch the workloads you want mirrored.

Once running, copycat begins replicating the images referenced by your workloads, respecting namespace and resource filters.

## Configuration

Copycat is configured through a combination of environment variables and a YAML configuration file. Any values defined in the environment override what is present in the config file, allowing safe secret management in Kubernetes.

### Environment variables

**Target selection**

- `TARGET_KIND`: `ecr` (default) or `docker`.
- `AWS_REGION`, `ECR_ACCOUNT_ID`, `ECR_REPO_PREFIX`, `ECR_CREATE_REPO`: configure AWS ECR mirroring.
- `TARGET_REGISTRY`, `TARGET_REPO_PREFIX`, `TARGET_USERNAME`, `TARGET_PASSWORD`, `TARGET_INSECURE`: configure other Docker registries.

**Workload selection**

- `INCLUDE_NAMESPACES`: `*` or a comma-separated list (for example `default,prod`).
- `SKIP_NAMESPACES`: namespaces that should never be mirrored.
- `SKIP_DEPLOYMENTS`, `SKIP_STATEFULSETS`, `SKIP_DAEMONSETS`, `SKIP_JOBS`, `SKIP_CRONJOBS`, `SKIP_PODS`: workload names to ignore.
- `WATCH_RESOURCES`: comma-separated resource types to watch (default `deployments,statefulsets,daemonsets,jobs,cronjobs,pods`).

**Registry routing**

- `EXCLUDE_REGISTRIES`: registry prefixes that should never be mirrored. Include the target registry (for example `123456789.dkr.ecr.eu-central-1.amazonaws.com`) to avoid loops.
- `TARGET_REPO_PREFIX` or config `repoPrefix`: prepend names before pushing to the destination.
- Optional `pathMap` entries in the config file rewrite repository paths before pushing.

**Mirroring behavior**

- `DIGEST_PULL`: resolve tags to digests before pulling (`false` by default).
- `CHECK_NODE_PLATFORM`: consult node architecture/OS before mirroring multi-arch images (`false` by default, requires `get` on `nodes`).
- `ALLOW_DIFFERENT_DIGEST_REPUSH`: permit overwriting tags with different digests (`true` by default, `latest` is always protected).
- `DRY_RUN`: perform all operations except pushing to the target registry (`false` by default).
- `DRY_PULL`: log which images would be fetched without contacting the source registry (`false` by default).

**Operations and observability**

- `REGISTRY_REQUEST_TIMEOUT`: timeout (in seconds) for individual pull/push operations (`120` by default).
- `FAILURE_COOLDOWN_MINUTES`: wait time before retrying a failed mirror (`1440` by default, `0` disables the cooldown).
- `METRICS_ADDR`: bind address for Prometheus metrics (`:8080` by default).
- `MAX_CONCURRENT_RECONCILES`: overrides the worker count per controller (defaults to `2`).

### Digest-based mirroring

Whether digest resolution is enabled fundamentally changes how copycat interacts with multi-architecture images:

- **`digestPull: true` / `DIGEST_PULL=true`** – copycat resolves the tag to its immutable digest. When reconciling Pods it prefers the digest reported by the kubelet in the container `ImageID`, guaranteeing that the mirrored artifact matches what actually runs on the node, even across architectures. When copycat only has a PodSpec (for example from a Deployment) it falls back to resolving the digest from the registry.
- **`digestPull: false` / default** – copycat keeps the original tag reference. When it encounters a manifest list (for example `alpine:3.19`), it downloads the entire multi-architecture index and uploads every referenced platform image to the target registry.

#### Alpine example

Assume a Pod references `docker.io/library/alpine:3.19`:

- With `digestPull=false`, copycat mirrors the manifest list and pushes layers for all available architectures (currently `386`, `amd64`, `arm64`, `ppc64le`, `riscv64`, and `s390x`) so the target registry can serve any of them.
- With `digestPull=true`, copycat mirrors the exact digest reported in the Pod’s status (for example `docker.io/library/alpine@sha256:...`). If the Pod runs on an `arm64` node, copycat mirrors the `arm64` manifest even when the controller executes on `amd64`.

This distinction matters when sizing storage in the mirror registry or when you rely on the `$arch` prefix placeholder described below.

### Watching workloads

Copycat listens to the Kubernetes resources you select. By default it watches Deployments, StatefulSets, DaemonSets, Jobs, CronJobs, and stand-alone Pods. You can narrow the scope through the `WATCH_RESOURCES` environment variable or the `watchResources` field in the configuration file. Unsupported entries are rejected at startup so you can catch typos early.

### Repository prefix templating

When a `repoPrefix` is configured (via config file or environment variables), the value can include placeholders that are replaced at runtime. The following tokens are available:

- `$namespace` — Namespace of the workload or Pod referencing the image.
- `$podname` — Name of the owning resource (or Pod when available).
- `$container_name` — Container name that uses the image.
- `$arch` — Architecture of the mirrored image. When `digestPull` is enabled this is the architecture of the selected manifest (for example `amd64`). When mirroring a manifest list, the placeholder expands to a hyphen-separated list of all mirrored architectures (for example `386-amd64-arm64-ppc64le-riscv64-s390x`). If copycat cannot determine the architecture it leaves the segment blank.

For example, setting `repoPrefix: "$namespace/$podname"` keeps target repositories unique across namespaces even when multiple workloads reference the same source image. To separate images by architecture you can combine placeholders:

```yaml
repoPrefix: "$arch/$namespace"
```

With the `alpine:3.19` example above this produces repositories such as `amd64/default/alpine` when `digestPull=true`, or `386-amd64-arm64-ppc64le-riscv64-s390x/default/alpine` when `digestPull=false` and the manifest list exposes all those variants.

### Lifecycle policies

You can provide an [ECR lifecycle policy](https://docs.aws.amazon.com/AmazonECR/latest/userguide/lifecycle_policy_examples.html) in the configuration file. When a repository is created by k8s-copycat, the policy is applied automatically.

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

### Example configuration

```yaml
digestPull: true                  # resolve source tags to their immutable digest before pulling
checkNodePlatform: true           # optional: ask the API for node architecture/OS before mirroring Pod images
mirrorPlatforms:                  # optional: always mirror these additional platforms when digestPull is enabled
  - amd64                         # shorthand for linux/amd64 also works
  - linux/arm64
allowDifferentDigestRepush: false # optional: fail when the target tag already exists with a different digest (except for "latest")
watchResources:
  - deployments                # default: listen to all supported resource types
  - statefulsets
  - daemonsets
  - jobs
  - cronjobs
  - pods
skipNamespaces: []               # default: allow all namespaces
skipNames:
  deployments: []               # default: watch every Deployment
  statefulSets: []              # default: watch every StatefulSet
  daemonSets: []                # default: watch every DaemonSet
  jobs: []                      # default: watch every Job
  cronJobs: []                  # default: watch every CronJob
  pods: []                      # default: watch every stand-alone Pod
maxConcurrentReconciles: 2       # default: two workers per controller
pathMap:
  - from: "group/project"
    to: "prod/project"
  - from: "^legacy/(.*)"
    to: "modern/$1"
    regex: true
requestTimeout: 120              # seconds; set to 0 to disable per-request deadlines
failureCooldownMinutes: 60       # retry failed pushes after one hour; set to 0 to disable the cooldown
forceReconcileMinutes: 30        # rescan all watched resources every 30 minutes; set to 0 to disable the periodic resync
registryCredentials:
  - registry: registry-1.docker.io
    registryAliases:
      - index.docker.io
      - docker.io
      - "*.docker.io"
    usernameEnv: DOCKERHUB_USERNAME
    passwordEnv: DOCKERHUB_PASSWORD
  - registry: ghcr.io
    registryAliases:
      - "*.ghcr.io"
      - docker.pkg.github.com
    tokenEnv: GHCR_TOKEN
```

Rules are evaluated in order, with the first matching entry applied. Leaving `pathMap` empty keeps repository paths unchanged. When `maxConcurrentReconciles` is omitted, copycat defaults to two workers per controller. You can override the value at runtime via the `MAX_CONCURRENT_RECONCILES` environment variable.

### Registry credentials

The `registryCredentials` section (or matching environment variables) lets copycat authenticate against private registries while mirroring into your target. Credentials can be supplied directly in the configuration file via `username`, `password`, or `token`, but referencing secret values through environment variables (`*Env` fields) is recommended. When a token is provided it is sent as an authentication bearer token; otherwise basic authentication is used.

## Troubleshooting mirrors

When you mirror or verify batches of image references—tags, digests, manifest lists, or attestations—transient errors should not block progress. If a particular reference fails to pull or push (missing credentials, non-runnable attestation, registry hiccup), skip it and continue. Copycat follows the same pattern internally: failures are recorded and retried later without preventing other objects from being mirrored. Emulate that workflow during manual checks by circling back once credentials or permissions have been corrected.

## Inspiration

- [estahn/k8s-image-swapper](https://github.com/estahn/k8s-image-swapper)
