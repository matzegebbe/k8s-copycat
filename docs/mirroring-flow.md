# Mirroring flow

```mermaid
flowchart TD
    A[Start mirror request] --> B{digestPull enabled?}
    B -- Yes --> C{Pod imageID includes digest?}
    B -- No --> V[[See flow below]]
    C -- Yes --> E{checkNodePlatform enabled?}
    C -- No --> D{Source reference already digest?}
    D -- No --> X[Skip reconciliation until digest is reported]
    D -- Yes --> I{mirrorPlatforms configured?}
    E -- Yes --> F[Look up node architecture/OS]
    E -- No --> G[Skip node lookup]
    F --> H{mirrorPlatforms configured?}
    G --> I
    H -- Yes --> J[Pull by digest and mirror configured platform subset]
    H -- No --> K[Pull by digest and select manifest matching node platform]
    I -- Yes --> J
    I -- No --> L{Target already exposes matching digest?}
    L -- Yes --> Z[End]
    L -- No --> M[Pull by digest and mirror entire index when descriptor is multi-arch]
    J --> N{Image present in target?}
    K --> N
    M --> N
    N -- No --> P[Ensure target repository exists]
    N -- Yes --> O{Digest matches source?}
    O -- Yes --> Z
    O -- No --> Q{Allowed to overwrite different digest? latest tag or allowDifferentDigestRepush}
    Q -- No --> R[Fail reconciliation and report mismatch]
    Q -- Yes --> P
    P --> S[Push image manifest to target]
    S --> T[Log push completion]
    R --> Z
    T --> Z
    X --> Z

```

```mermaid
flowchart TD
    A[Start mirror request] --> B{digestPull enabled?}
    B -- No --> C[Pull source image or index by tag]
    C --> D{Descriptor is multi-arch index?}
    D -- Yes --> E[Mirror entire index]
    D -- No --> F[Mirror single image]
    E --> G[Ensure target repository exists]
    F --> G
    G --> H{Image present in target?}
    H -- No --> I[Push image or index to target]
    H -- Yes --> J{Digest matches source?}
    J -- Yes --> K[Skip push and finish]
    J -- No --> L{Allowed to overwrite different digest? latest tag or allowDifferentDigestRepush}
    L -- No --> M[Fail reconciliation and report mismatch]
    L -- Yes --> I
    I --> N[Log push completion]
    K --> O[End]
    N --> O
    M --> O

```

This flow chart summarizes how the pusher reconciles images when digest mirroring is enabled versus disabled.

## Why “pull by digest” can still copy other variants

When `digestPull` is enabled it might feel like the controller should only ever copy the single manifest identified by the digest the workload runs. In practice Kubernetes and container runtimes often publish the *index* digest for multi-architecture images, so additional context is required to avoid mirroring every variant. When Copycat only has a digest reference (for example, the workload image already uses an `@sha256:` reference) but the pod has not reported its `ImageID` digest yet, the controller now checks the target registry first and skips contacting the source registry entirely if the digest already matches.

### Why other manifests are mirrored

- Kubernetes reports each container’s `ImageID`, and with `digestPull` enabled copycat rewrites the source reference to that digest before contacting the registry.
- For many multi-architecture images (for example, the `nginx:1.28` release), the digest stored in `ImageID` is the **index** digest, not the per-platform manifest digest. Resolving that digest returns an OCI index that still contains every platform variant plus attestations.
- Without the node’s platform, copycat has no hint to pass to `remote.WithPlatform`, so it mirrors the whole index to make sure every architecture remains available. That keeps the right image ready even if the running workload migrates to a different node type later.

### How node metadata avoids foreign digests

- When the reconciler includes the node’s architecture/OS in the metadata, the pusher can call `remote.WithPlatform` and select the matching descriptor from the multi-arch index instead of mirroring the full list.
- With that hint, the only manifest mirrored is the one your node actually runs, so foreign digests no longer appear in the target registry.

Enable this behaviour with the `checkNodePlatform` config option (or the `CHECK_NODE_PLATFORM` environment variable). When it is enabled the controller needs RBAC permission to `get` core `nodes` so it can read the scheduled node's platform details. When the option is disabled—the default—the controller mirrors every runnable manifest from the index to keep all architectures in sync.

If you regularly need multiple platforms regardless of where workloads land, declare them explicitly with the `mirrorPlatforms` configuration (or the `MIRROR_PLATFORMS` environment variable). When `digestPull` is enabled, copycat mirrors the subset of manifests that match the configured platforms and any reported node platform. If `checkNodePlatform` surfaces a platform that is not in the configured list, copycat logs a warning so you can decide whether to add it.

### When digest-only would work without platform data

- If the runtime surfaced the exact per-platform manifest digest (for example, `nginx@sha256:10f4…`) in `ImageID`, the registry lookup would directly return the single manifest image and platform hints would be unnecessary.
- In practice, Kubernetes/containerd usually publish the index digest instead, so copycat needs extra context to decide which manifest to mirror.
