# Installation

## Package managers

```bash
# kubectl krew
kubectl krew install cilium-policy-gen

# go install
go install github.com/SoulKyu/cpg/cmd/cpg@latest
```

When installed via krew, use `kubectl cilium-policy-gen` instead of `cpg`. Same flags, same behavior.

## From source

```bash
git clone https://github.com/SoulKyu/cpg.git
cd cpg
make build
# binary lands in ./bin/cpg
```

Requires Go 1.25+ for source builds.

## Supported Cilium versions

cpg targets **Cilium >= 1.14**. Clusters below that floor are detected at
connect time and logged as a warning -- never blocked, so reduced-RBAC and
CI service accounts still get a working (if unverified) run.

Individual capabilities carry their own, higher floors:

| Feature | Cilium version | Notes |
|---------|-----------------|-------|
| Baseline cpg operation | >= 1.14 | Declared floor -- the lowest version any shipped code path assumes |
| `PolicyVerdictNotify` audit-action bit | >= 1.8 | PR [#11843](https://github.com/cilium/cilium/pull/11843) |
| `Verdict_AUDIT` via the Hubble flow API (`--include-audit`) | >= 1.10 | PR [#14785](https://github.com/cilium/cilium/pull/14785) / [#14923](https://github.com/cilium/cilium/pull/14923) |
| `cilium-dbg` binary naming (was `cilium`) | >= 1.15 | PR [#28085](https://github.com/cilium/cilium/pull/28085) |
| `enableDefaultDeny` CNP field | >= 1.16 | PR [#30572](https://github.com/cilium/cilium/pull/30572) -- used by `cpg bootstrap` / `get_bootstrap_policy`; see the [bootstrap runbook](bootstrap-runbook.md) |
| `policy.cilium.io/proxy-visibility` annotation | <= 1.16 | Removed from the agent runtime at 1.17 -- PR [#35019](https://github.com/cilium/cilium/pull/35019) |
| Observer `GetNodes()` RPC / `version` field | >= 1.10 | PR [#13979](https://github.com/cilium/cilium/pull/13979) |

## k9s plugin

You can trigger cpg directly from k9s on a namespace. Drop this into `$XDG_CONFIG_HOME/k9s/plugins.yaml` (usually `~/.config/k9s/plugins.yaml`):

```yaml
plugins:
  cpg:
    shortCut: Shift-G
    description: Generate Cilium policies from dropped flows
    scopes:
    - namespace
    command: cpg
    background: false
    args:
    - generate
    - -n
    - $NAME
    - --cluster-dedup
```

Navigate to a namespace in k9s, press `Shift-G`, and cpg starts streaming dropped flows for that namespace. Ctrl+C to stop -- policies land in `./policies/<namespace>/`.

If you installed via krew instead of `go install`, replace `command: cpg` with `command: kubectl` and prepend `cilium-policy-gen` to the args:

```yaml
plugins:
  cpg:
    shortCut: Shift-G
    description: Generate Cilium policies from dropped flows
    scopes:
    - namespace
    command: kubectl
    background: false
    args:
    - cilium-policy-gen
    - generate
    - -n
    - $NAME
    - --cluster-dedup
```

## Next steps

- [Getting started](getting-started.md) — first live capture and offline replay
- [Bootstrap runbook](bootstrap-runbook.md) — onboarding a namespace to default-deny
