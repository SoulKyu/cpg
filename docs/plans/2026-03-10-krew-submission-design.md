# CPG krew plugin submission design

## Decisions

| Topic | Decision |
|-------|----------|
| Plugin name | `cilium-policy-gen` (`kubectl cilium-policy-gen`) |
| Scope | Direct invocation, no sub-commands (equivalent to `cpg generate`) |
| Platforms | linux/amd64, linux/arm64, darwin/amd64, darwin/arm64 |
| Binary rename | GoReleaser renames to `kubectl-cilium_policy_gen`, source stays `cmd/cpg/` |
| Release automation | GoReleaser + GitHub Actions on tag push |
| Krew updates | krew-release-bot auto-PRs to krew-index |
| License | Apache 2.0 |

## Changes to CPG repo

### 1. LICENSE file (Apache 2.0)

Add `LICENSE` at repo root. GoReleaser includes it in archives automatically.

### 2. kubectl detection in cmd/cpg/main.go

Detect if `os.Args[0]` contains `kubectl-` prefix. When invoked as kubectl
plugin, adapt usage message to show `kubectl cilium-policy-gen` instead of
`cpg`. Default behavior without sub-command is `generate`.

### 3. .goreleaser.yaml

- Build `cmd/cpg/` for 4 targets
- Rename binary to `kubectl-cilium_policy_gen` in archives
- Archive format: `cilium-policy-gen_<os>_<arch>.tar.gz`
- Include LICENSE in archives
- Generate sha256 checksums

### 4. .github/workflows/release.yml

- Trigger on `v*` tags
- Run GoReleaser to build and publish GitHub release
- Archives + checksums attached to release

### 5. .krew.yaml (template)

Template manifest for krew-release-bot with placeholders for version, sha256,
and download URIs. The bot fills these on each release.

### 6. .github/workflows/krew-update.yml

Workflow using krew-release-bot to open PR on kubernetes-sigs/krew-index
when a new GitHub release is published.

## krew-index manifest

File: `plugins/cilium-policy-gen.yaml` in kubernetes-sigs/krew-index.

```yaml
apiVersion: krew.googlecontainertools.github.com/v1alpha2
kind: Plugin
metadata:
  name: cilium-policy-gen
spec:
  version: v1.2.0
  homepage: https://github.com/SoulKyu/cpg
  shortDescription: "Generate CiliumNetworkPolicy from dropped Hubble flows"
  description: |
    Watches dropped network flows in real-time via Hubble Relay and
    automatically generates CiliumNetworkPolicy YAML files that would
    allow them. Useful for clusters with default-deny posture where
    writing policies manually is tedious and error-prone.

    Supports TCP/UDP ports, ICMP, reserved entities, and CIDR rules.
    Merges with existing policies on disk and writes only on changes.
  caveats: |
    Requires a running Cilium cluster with Hubble Relay enabled.
    The plugin auto port-forwards to hubble-relay by default.
  platforms:
  - selector:
      matchLabels:
        os: linux
        arch: amd64
    uri: https://github.com/SoulKyu/cpg/releases/download/v1.2.0/cilium-policy-gen_linux_amd64.tar.gz
    sha256: "FILLED_BY_RELEASE_BOT"
    bin: kubectl-cilium_policy_gen
  - selector:
      matchLabels:
        os: linux
        arch: arm64
    uri: https://github.com/SoulKyu/cpg/releases/download/v1.2.0/cilium-policy-gen_linux_arm64.tar.gz
    sha256: "FILLED_BY_RELEASE_BOT"
    bin: kubectl-cilium_policy_gen
  - selector:
      matchLabels:
        os: darwin
        arch: amd64
    uri: https://github.com/SoulKyu/cpg/releases/download/v1.2.0/cilium-policy-gen_darwin_amd64.tar.gz
    sha256: "FILLED_BY_RELEASE_BOT"
    bin: kubectl-cilium_policy_gen
  - selector:
      matchLabels:
        os: darwin
        arch: arm64
    uri: https://github.com/SoulKyu/cpg/releases/download/v1.2.0/cilium-policy-gen_darwin_arm64.tar.gz
    sha256: "FILLED_BY_RELEASE_BOT"
    bin: kubectl-cilium_policy_gen
```

## Execution order

1. Add LICENSE (Apache 2.0)
2. Adapt cmd/cpg/main.go for kubectl detection
3. Create .goreleaser.yaml
4. Create .github/workflows/release.yml
5. Create .krew.yaml template
6. Create .github/workflows/krew-update.yml
7. Tag release (v1.2.0) to trigger pipeline
8. Submit initial PR to kubernetes-sigs/krew-index
