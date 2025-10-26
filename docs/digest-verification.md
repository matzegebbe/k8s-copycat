# Verifying Image Digests with `skopeo`

This guide shows how to confirm the digest of an image, understand the values reported for multi-architecture versus single-architecture images, and relate those values to the `imageID` that Kubernetes records for running containers.

## Checking a tag's digest

`skopeo` can inspect remote registries without pulling layers locally. Use the `inspect` command to resolve a tag to a digest:

```bash
skopeo inspect docker://docker.io/library/alpine:3.19 --format '{{.Digest}}'
```

The digest reported here corresponds to the manifest object addressed by the tag. For multi-architecture images this will usually be the manifest list (an OCI image index), while single-architecture images will resolve directly to an image manifest.

## Exploring multi-architecture manifest lists

When a tag points at a manifest list, you can inspect the per-platform entries by querying the raw JSON and piping the result through `jq`:

```bash
skopeo inspect --raw docker://docker.io/library/alpine:3.19 \
  | jq '.manifests[] | {platform, digest}'
```

Each item contains a digest that identifies the platform-specific image manifest. These digests are the values the kubelet reports in `.status.containerStatuses[].imageID` when your node pulls the matching architecture.

Manifest lists can contain non-runtime artifacts as well. For example, the `quay.io/jitesoft/alpine:3.20.8` index includes entries that advertise `"vnd.docker.reference.type": "attestation-manifest"` with an `unknown/unknown` platform:

```bash
skopeo inspect --raw docker://quay.io/jitesoft/alpine:3.20.8 \
  | jq '.manifests[] | select(.annotations."vnd.docker.reference.type"=="attestation-manifest")'
```

These attestations are used for supply-chain metadata (such as signatures or SBOMs); they are not runnable images. Container runtimes ignore them when resolving a platform, so you can safely skip over them when you only need the digest of the executable image manifest.

To focus on a single platform, combine `--format` with `--raw` to fetch the manifest and compute its digest:

```bash
skopeo inspect --raw docker://docker.io/library/alpine@sha256:<per-platform-digest> \
  | sha256sum
```

The output hash corresponds to the digest displayed in the manifest list entry because OCI digests are the SHA-256 of the canonical JSON representation of the manifest.

### Resolving a tag directly to a per-platform digest

When you need the digest of the manifest that a particular runtime will pull, `skopeo` can resolve the manifest list for you. Specify the desired platform with `--override-arch` and `--override-os`:

```bash
skopeo inspect \
  --override-arch amd64 \
  --override-os linux \
  docker://quay.io/jitesoft/alpine:3.20.8 \
  --format '{{.Digest}}'
```

The digest in the output (for example, `sha256:a1b46a5d67877b1aab2fdca9d4b7d2ff2cac14b820f60cecacbcbb2208dcf7b4`) is the exact value containerd and Kubernetes will record when pulling the linux/amd64 variant of this tag. This is typically the easiest way to mirror or pin the image you see running in a cluster.

If you prefer to keep working from the manifest list, first select the platform entry and then inspect it directly:

```bash
skopeo inspect --raw docker://quay.io/jitesoft/alpine:3.20.8 \
  | jq -r '.manifests[] | select(.platform.architecture=="amd64" and .platform.os=="linux") | .digest'

skopeo inspect docker://quay.io/jitesoft/alpine@sha256:95878558b19d60a174a65b6353e0739af53a7b601442ae737725d9bdd6d5e163 \
  --format '{{.Digest}}'
```

The first command identifies the manifest list entry for `linux/amd64`; the second inspects that manifest. Its reported digest again matches the runtime's `imageID`, proving that the per-platform manifest and the `imageID` share the same canonical hash.

If you need to query the registry API directly—useful when building automation or verifying what a mirroring controller uploaded—you can request the OCI manifest with the appropriate media type. The registry responds with a `Docker-Content-Digest` header that carries the authoritative hash:

```bash
curl -sSf \
  -H 'Accept: application/vnd.oci.image.manifest.v1+json' \
  https://quay.io/v2/jitesoft/alpine/manifests/sha256:95878558b19d60a174a65b6353e0739af53a7b601442ae737725d9bdd6d5e163 \
  -D - \
  | grep Docker-Content-Digest
# Docker-Content-Digest: sha256:a1b46a5d67877b1aab2fdca9d4b7d2ff2cac14b820f60cecacbcbb2208dcf7b4
```

The digest in this header matches the value that `skopeo inspect --override-arch ...` reports, confirming you are looking at the platform-specific manifest rather than the top-level index.

## Single-architecture images

If a tag references only one architecture, `skopeo inspect --format '{{.Digest}}'` yields the same digest that appears when you inspect the manifest directly. There is no manifest list wrapper, so the tag digest and the image digest are identical.

## Understanding Kubernetes `imageID`

Kubernetes surfaces the exact object the container runtime pulled via the `imageID` field:

```bash
kubectl get pod <name> -n <namespace> \
  -o jsonpath='{range .status.containerStatuses[*]}{@.name}={@.imageID}{"\n"}{end}'
```

The runtime reports the resolved image reference, including the digest of the manifest it stored locally. On multi-architecture images this will match one of the platform entries from the manifest list above; on single-architecture images it matches the tag digest directly. This value is what copycat uses when `digestPull` is enabled, ensuring the mirrored artifact aligns exactly with what the kubelet executed.

## Why an amd64 digest appears on Apple Silicon clusters

When running Kubernetes-in-Docker solutions such as `kind` on macOS with Podman or Docker Desktop, the node images are often built for `linux/amd64`. Even though the host hardware is `arm64`, the node container itself runs under emulation (via `qemu-user`) and presents `x86_64` to the embedded container runtime. You can verify this by exec'ing into a node and checking the reported architecture:

```bash
kubectl exec -n kube-system kind-control-plane -- uname -m
# x86_64
```

Because containerd inside the node believes it is an amd64 machine, it selects the `linux/amd64` manifest from a multi-architecture image index, resulting in the amd64 digest you observe in `kubectl describe` output and in mirroring tools. To consume native arm64 images inside `kind`, choose an arm64 node image when creating the cluster:

```bash
kind create cluster --image kindest/node:v1.30.0@sha256:<arm64-node-image-digest>
```

Alternatively, run `kind` on an environment (such as Colima or Lima) that provisions arm64 node images by default so that containerd resolves tags to the `linux/arm64` manifests instead.

## Troubleshooting pull and push failures while iterating digests

When you mirror or verify multiple digests in succession, transient errors should not block progress. If a particular manifest fails to pull or push—for example, due to missing credentials or an attestation entry that does not contain runnable image data—move on to the next manifest in the list and continue processing. Copycat follows this pattern internally: failures are recorded and retried later without preventing other objects from being mirrored. You can emulate that workflow during manual checks by skipping problematic entries and circling back once credentials or permissions have been corrected.
