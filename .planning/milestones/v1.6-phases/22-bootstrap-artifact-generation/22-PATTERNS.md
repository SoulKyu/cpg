# Phase 22: Bootstrap Artifact Generation - Pattern Map

**Mapped:** 2026-07-22  
**Files analyzed:** 5 new/modified files  
**Analogs found:** 5 / 5 (100%)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `cmd/cpg/bootstrap.go` | controller | request-response | `cmd/cpg/replay.go` | exact |
| `pkg/policy/bootstrap_builder.go` (or similar internal builder) | service | transform | `pkg/policy/builder.go` | exact |
| `cmd/cpg/mcp_tools.go` (modify to add `get_bootstrap_policy`) | middleware | request-response | `cmd/cpg/mcp_query.go` | exact |
| `cmd/cpg/*bootstrap*_test.go` | test | CRUD | `pkg/policy/builder_test.go` + `cmd/cpg/generate_test.go` | exact |
| `README.md` (add compat row) | config | n/a | `cmd/cpg/readme_compat_test.go` | role-match |
| `docs/bootstrap-runbook.md` | documentation | n/a | `docs/KNOWN_LIMITATIONS.md` structure | structural |

## Pattern Assignments

### `cmd/cpg/bootstrap.go` (controller, request-response)

**Analog:** `cmd/cpg/replay.go` (simplest command pattern — no streaming, clean flag handling)

**Command Constructor & Registration** (replay.go lines 20-46, main.go lines 57-60):

```go
// Pattern: newXCmd() constructor
func newBootstrapCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:     "bootstrap",
        PreRunE: validateBootstrapFlags,  // validate before kubeconfig load
        Short:   "Generate a namespaced default-deny bootstrap policy",
        Long: `Generate a CiliumNetworkPolicy that enforces default-deny on a namespace,
with explicit enableDefaultDeny field and empty ingress/egress stanzas...`,
        Args: cobra.NoArgs,  // bootstrap takes no positional args
        RunE: runBootstrap,
    }

    // Add flags following the pattern
    cmd.Flags().StringP("namespace", "n", "", "target namespace (required)")
    cmd.Flags().StringP("output", "o", "", "output file (default: stdout)")
    
    return cmd
}

// Wire into main.go at line 57-60:
rootCmd.AddCommand(newBootstrapCmd())
```

**Namespace Flag Pattern** (commonflags.go lines 68-69, generate_test.go lines 23-52):

```go
// Required namespace with no default, no all-namespaces variant
cmd.Flags().StringP("namespace", "n", "", "target namespace (required)")

// Validation in PreRunE (validate BEFORE any cluster/filesystem access)
func validateBootstrapFlags(cmd *cobra.Command, _ []string) error {
    ns, _ := cmd.Flags().GetString("namespace")
    if ns == "" {
        return fmt.Errorf("--namespace/-n is required")
    }
    // namespace format already validated by preflight; no need to re-check
    return nil
}
```

**Output Flag Pattern** (generate.go lines 142-144, pkg/output/writer.go lines 81-102):

```go
// Mimic generate's -o pattern: stdout by default, file if provided
cmd.Flags().StringP("output", "o", "", "output file (default: stdout)")

// In runBootstrap, after artifact generation:
output := <generated YAML bytes>
if outputPath, _ := cmd.Flags().GetString("output"); outputPath != "" {
    // Use pkg/output atomic writer
    w := output.NewWriter(filepath.Dir(outputPath), logger)
    // But bootstrap is simple enough to just write once via atomic temp+rename
    // (same pattern as writer.go lines 81-102)
    tmpFile, err := os.CreateTemp(filepath.Dir(outputPath), basename(outputPath)+".tmp-*")
    // ... write, close, chmod, rename (see writer.go 81-102)
} else {
    // Pipe-friendly: write to stdout
    fmt.Println(string(output))
}
```

**Version Preflight Pattern** (generate.go lines 76-96, version.go lines 147-177):

```go
// Same warn-and-proceed philosophy as generate.go
func runBootstrap(cmd *cobra.Command, _ []string) error {
    f := parseBootstrapFlags(cmd)  // custom flags struct
    
    ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
    defer cancel()
    
    // Always-on version detection (like generate.go line 269)
    kubeConfig, _ := k8s.LoadKubeConfig()  // may fail; that's fine
    if kubeConfig != nil {
        client, _ := kubernetes.NewForConfig(kubeConfig)
        if client != nil {
            // This runs the DetectCiliumVersion preflight
            k8s.DetectCiliumVersion(ctx, client, logger)
            // Logger emits the "below floor" warning internally (version.go:363)
            // Operator must see: hard refusal if determined version < 1.16
            // OR warn-and-proceed if undetermined
        }
    }
    
    // Then proceed with artifact generation
    artifact := buildBootstrapPolicy(f.namespace)
    // ... write artifact (see output flag pattern above)
}
```

**Error Handling Pattern** (replay.go lines 49-73):

```go
// Match replay.go's clean error flow: return early, let cobra handle exit
func runBootstrap(cmd *cobra.Command, _ []string) error {
    f := parseBootstrapFlags(cmd)
    
    // Validation errors first
    if err := f.validate(); err != nil {
        return err  // cobra converts to exit 1
    }
    
    // Then kubeconfig/cluster errors (these are logged, return-as-is)
    kubeConfig, err := k8s.LoadKubeConfig()
    if err != nil && outputPath == "" {
        // only fail if we can't write to file AND can't reach cluster
        return fmt.Errorf("kubeconfig not available: %w", err)
    }
    
    // Artifact generation errors
    cnp, err := buildBootstrapPolicy(namespace)
    if err != nil {
        return err
    }
    
    // Write errors propagate
    return writeBootstrapArtifact(cnp, outputPath)
}
```

---

### `pkg/policy/bootstrap_builder.go` (service, transform)

**Analog:** `pkg/policy/builder.go` (policy building patterns)

**Imports Pattern** (builder.go lines 1-16):

```go
package policy

import (
    "sort"
    "strconv"
    "strings"
    "time"

    flowpb "github.com/cilium/cilium/api/v1/flow"
    ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
    "github.com/cilium/cilium/pkg/policy/api"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/util/intstr"

    "github.com/SoulKyu/cpg/pkg/labels"
)
```

**Core Builder Function** (builder.go lines 91-144):

```go
// Pattern: public Builder function + internal field initialization
func BuildBootstrapPolicy(namespace string) *ciliumv2.CiliumNetworkPolicy {
    cnp := &ciliumv2.CiliumNetworkPolicy{
        TypeMeta: metav1.TypeMeta{
            APIVersion: "cilium.io/v2",
            Kind:       "CiliumNetworkPolicy",
        },
        ObjectMeta: metav1.ObjectMeta{
            Name:      "default-deny-" + namespace,  // literal naming per CONTEXT.md
            Namespace: namespace,
            Labels: map[string]string{
                "app.kubernetes.io/managed-by": "cpg",
            },
        },
        Spec: &api.Rule{
            // Empty endpoint selector — selects all endpoints in namespace
            EndpointSelector: api.NewESFromMatchRequirements(nil),
            
            // CRITICAL: explicit enableDefaultDeny + empty stanzas (cilium#35558)
            EnableDefaultDeny: &api.DefaultDeny{
                Ingress: true,
                Egress:  true,
            },
            // Empty stanzas — both present, tested as success criterion 1
            Ingress: []*api.IngressRule{},
            Egress:  []*api.EgressRule{},
        },
    }
    return cnp
}
```

**YAML Marshaling** (builder.go uses sigs.k8s.io/yaml; pkg/output/writer.go lines 56-74):

```go
// Pattern: reuse the exact marshal path as generate for byte-consistent style
import "sigs.k8s.io/yaml"

// In the command handler or writer:
data, err := yaml.Marshal(cnp)
if err != nil {
    return fmt.Errorf("marshaling policy: %w", err)
}
// Write data to stdout or file (see output flag pattern above)
```

---

### `cmd/cpg/mcp_tools.go` (modify to add readonly tool)

**Analog:** `cmd/cpg/mcp_query.go` (readonly tool registration + handler pattern)

**Readonly Tool Registration Pattern** (mcp_query.go lines 42-79, mcp_tools.go lines 96-145):

```go
// Add to cmd/cpg/mcp_tools.go or a new cmd/cpg/mcp_bootstrap.go:

func registerBootstrapTool(server *mcp.Server, mgr *session.Manager) {
    mcp.AddTool(server, &mcp.Tool{
        Name: "get_bootstrap_policy",
        Description: "Generate a namespaced default-deny bootstrap " +
            "CiliumNetworkPolicy (enableDefaultDeny + explicit empty ingress/egress). " +
            "Detects the cluster's Cilium version and returns the artifact directly " +
            "as YAML content, along with detected version and compatibility info. " +
            "Returns hard error if version < 1.16 (determined); warns and proceeds " +
            "if version is undetermined (cluster unreachable, reduced RBAC).",
        Annotations: &mcp.ToolAnnotations{
            ReadOnlyHint: true,  // Never mutates cluster or writes to filesystem
            IdempotentHint: true,  // Same inputs → same output
            OpenWorldHint: false,  // Fully deterministic, no edge cases
        },
    }, func(ctx context.Context, _ *mcp.CallToolRequest, args bootstrapArgs) (*mcp.CallToolResult, bootstrapResult, error) {
        return handleGetBootstrapPolicy(mgr, args)
    })
}

// Wire into mcp.go's runMCPServer composition root (mirroring registerQueryTools line 38):
// (after registerSessionTools, before or after registerQueryTools)
registerBootstrapTool(server, mgr)
```

**Argument & Result Shapes** (mcp_query.go lines 147-164, 276-293):

```go
// Input schema (minimal, matching bootstrap's simplicity)
type bootstrapArgs struct {
    Namespace string `json:"namespace" jsonschema:"target namespace (required)"`
    // Version detection is automatic; no explicit field needed
}

// Output schema (YAML content + version compat info)
type bootstrapResult struct {
    Namespace         string   `json:"namespace"`
    YAML              string   `json:"yaml" jsonschema:"the full CiliumNetworkPolicy YAML"`
    ClusterVersion    string   `json:"cluster_version" jsonschema:"detected Cilium version, or empty if undetermined"`
    VersionSource     string   `json:"version_source" jsonschema:"'pod-images', 'get-nodes', or 'undetermined'"`
    BelowFloorFeatures []string `json:"below_floor_features,omitempty" jsonschema:"feature floors not met, if any"`
    VersionWarning    string   `json:"version_warning,omitempty" jsonschema:"actionable message if version is below 1.16 or undetermined"`
}
```

**Handler Pattern** (mcp_query.go lines 166-248):

```go
// Mimic handleListPolicies / handleGetPolicy structure
func handleGetBootstrapPolicy(mgr *session.Manager, args bootstrapArgs) (*mcp.CallToolResult, bootstrapResult, error) {
    // Input validation (D-16: validate before I/O)
    if args.Namespace == "" {
        return nil, bootstrapResult{}, fmt.Errorf("namespace is required")
    }
    
    // Generate the artifact (no filesystem I/O, no cluster mutation)
    cnp := policy.BuildBootstrapPolicy(args.Namespace)
    yamlBytes, err := yaml.Marshal(cnp)
    if err != nil {
        return nil, bootstrapResult{}, fmt.Errorf("marshaling policy: %w", err)
    }
    
    // Run version detection (advisory, never fails; mirrors generate.go:269)
    // Create a minimal kubeconfig-load for version detection
    kubeConfig, err := k8s.LoadKubeConfig()
    if err != nil {
        // Undetermined version: proceed
        return nil, bootstrapResult{
            Namespace: args.Namespace,
            YAML: string(yamlBytes),
            VersionSource: "undetermined",
            VersionWarning: "version detection unavailable; enableDefaultDeny requires Cilium >= 1.16",
        }, nil
    }
    
    client, _ := kubernetes.NewForConfig(kubeConfig)
    if client == nil {
        return nil, bootstrapResult{
            Namespace: args.Namespace,
            YAML: string(yamlBytes),
            VersionSource: "undetermined",
            VersionWarning: "version detection unavailable; enableDefaultDeny requires Cilium >= 1.16",
        }, nil
    }
    
    // Detect version (this call never fails; it emits warnings internally)
    compat := k8s.DetectCiliumVersion(ctx, client, logger)
    
    // Hard error if determined but below floor
    if compat.ClusterVersion != "" && len(compat.BelowFloorFeatures) > 0 {
        // Check if enableDefaultDeny is in the below-floor list
        for _, feature := range compat.BelowFloorFeatures {
            if strings.Contains(feature, "enableDefaultDeny") {
                return nil, bootstrapResult{}, fmt.Errorf(
                    "cluster Cilium version %s does not support bootstrap (requires >= 1.16); "+
                    "affected features: %s",
                    compat.ClusterVersion, strings.Join(compat.BelowFloorFeatures, "; "),
                )
            }
        }
    }
    
    // Success: return artifact + version info for LLM reasoning
    return nil, bootstrapResult{
        Namespace: args.Namespace,
        YAML: string(yamlBytes),
        ClusterVersion: compat.ClusterVersion,
        VersionSource: compat.Source,
        BelowFloorFeatures: compat.BelowFloorFeatures,
    }, nil
}
```

---

## Shared Patterns

### Version Detection & Gating (Phase 21 integration)

**Source:** `pkg/k8s/version.go` (lines 97-104, 147-177, 352-372)

**Apply to:** Both `cmd/cpg/bootstrap.go` runBootstrap and MCP tool handler

```go
// featureFloors table already includes enableDefaultDeny at 1.16.0 (line 103):
var featureFloors = []struct {
    name  string
    floor string
}{
    {"baseline cpg operation", "1.14.0"},
    {"cilium-dbg binary naming", "1.15.0"},
    {"enableDefaultDeny CNP field", "1.16.0"},  // ← Phase 21 already added this
}

// Always-on, warn-and-proceed philosophy:
// - Determined version < 1.16 → hard error (planner decision)
// - Undetermined version (no cluster, RBAC-denied) → warn-and-proceed
// - Determined version >= 1.16 → proceed silently

// Error message style (k8s/version.go:54-55):
const warnBelowFloorPrefix = "version preflight: cluster Cilium version is below one or more feature floors; proceeding without blocking. "
```

### Validation Before I/O (Pitfall J pattern)

**Source:** `cmd/cpg/commonflags.go` (lines 189-251), `cmd/cpg/mcp_query.go` (lines 172-183)

**Apply to:** `cmd/cpg/bootstrap.go` flags validation + MCP tool argument validation

```go
// PreRunE for CLI: runs BEFORE kubeconfig load / port-forward
func validateBootstrapFlags(cmd *cobra.Command, _ []string) error {
    ns, _ := cmd.Flags().GetString("namespace")
    if ns == "" {
        return fmt.Errorf("--namespace/-n is required")
    }
    return nil
}

// Handler for MCP: validate input BEFORE any filesystem/cluster access
func handleGetBootstrapPolicy(...) {
    if args.Namespace == "" {
        return nil, bootstrapResult{}, fmt.Errorf("namespace is required")
    }
    // ... proceed with artifact generation
}
```

### Atomic File Writing

**Source:** `pkg/output/writer.go` (lines 81-102)

**Apply to:** `cmd/cpg/bootstrap.go` when `-o/--output` is provided

```go
// Pattern: create temp → write → chmod → rename (atomic)
tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
if err != nil {
    return fmt.Errorf("creating temp file: %w", err)
}
tmpPath := tmp.Name()
if _, err := tmp.Write(data); err != nil {
    _ = tmp.Close()
    _ = os.Remove(tmpPath)
    return fmt.Errorf("writing temp file: %w", err)
}
if err := tmp.Close(); err != nil {
    _ = os.Remove(tmpPath)
    return fmt.Errorf("closing temp file: %w", err)
}
if err := os.Chmod(tmpPath, 0644); err != nil {
    _ = os.Remove(tmpPath)
    return fmt.Errorf("setting permissions: %w", err)
}
if err := os.Rename(tmpPath, path); err != nil {
    _ = os.Remove(tmpPath)
    return fmt.Errorf("atomic rename: %w", err)
}
```

---

## Test Patterns

### Named Acceptance Test: enableDefaultDeny + Empty Stanzas Roundtrip

**Analog:** `pkg/policy/builder_test.go` (YAML roundtrip pattern, lines 420+)

**Create as:** `pkg/policy/builder_bootstrap_test.go` or `cmd/cpg/bootstrap_test.go`

```go
package policy_test

import (
    "testing"
    "github.com/stretchr/testify/require"
    "sigs.k8s.io/yaml"
    "github.com/SoulKyu/cpg/pkg/policy"
)

// TestBuildBootstrapPolicy_EnableDefaultDenyAndEmptyStanzasRoundtrip
// Named test asserting the cilium#35558 regression guard:
// BOTH enableDefaultDeny field AND explicit empty stanzas survive marshaling.
func TestBuildBootstrapPolicy_EnableDefaultDenyAndEmptyStanzasRoundtrip(t *testing.T) {
    cnp := policy.BuildBootstrapPolicy("test-ns")
    require.NotNil(t, cnp)
    
    // Marshal to YAML (same path as kubectl apply uses)
    data, err := yaml.Marshal(cnp)
    require.NoError(t, err)
    
    // Unmarshal back in-memory
    var unmarshaled ciliumv2.CiliumNetworkPolicy
    err = yaml.Unmarshal(data, &unmarshaled)
    require.NoError(t, err)
    
    // SUCCESS CRITERION 1: both fields present and unchanged
    require.NotNil(t, unmarshaled.Spec.EnableDefaultDeny)
    assert.True(t, unmarshaled.Spec.EnableDefaultDeny.Ingress)
    assert.True(t, unmarshaled.Spec.EnableDefaultDeny.Egress)
    
    // SUCCESS CRITERION 1: explicit empty stanzas
    assert.Empty(t, unmarshaled.Spec.Ingress)
    assert.Empty(t, unmarshaled.Spec.Egress)
    
    // SUCCESS CRITERION 1: endpoint selector selects all (empty means all in namespace)
    assert.Empty(t, unmarshaled.Spec.EndpointSelector.LabelSelector.MatchLabels)
}
```

### README Compatibility Table Pin

**Analog:** `cmd/cpg/readme_compat_test.go` (lines 31-72)

**Add to existing:** `cmd/cpg/readme_compat_test.go` (extend TestReadmeCompatSection)

```go
// In TestReadmeCompatSection (existing test), extend the version token loop:
func TestReadmeCompatSection(t *testing.T) {
    data, err := os.ReadFile("../../README.md")
    require.NoError(t, err)
    readme := string(data)
    
    // Existing checks...
    
    // NEW: Pin the bootstrap/enableDefaultDeny row
    assert.Contains(t, readme, "enableDefaultDeny",
        "README compat table must mention enableDefaultDeny feature")
    
    assert.Contains(t, readme, "1.16",
        "README compat table must list 1.16 as enableDefaultDeny floor")
    
    assert.Contains(t, readme, "#30572",
        "README compat table must cite merged PR #30572 backing enableDefaultDeny")
}
```

**README.md Changes:**

Add or update the Supported Cilium versions table (line 64-80) to include or verify:

```markdown
| `enableDefaultDeny` CNP field | >= 1.16 | PR [#30572](https://github.com/cilium/cilium/pull/30572) |
```

### MCP Tool Tests

**Analog:** `cmd/cpg/mcp_query_tools_test.go` (lines 33-88)

**Create as:** `cmd/cpg/mcp_bootstrap_test.go` or extend `cmd/cpg/mcp_query_tools_test.go`

```go
package main

import (
    "context"
    "testing"
    "github.com/modelcontextprotocol/go-sdk/mcp"
    "github.com/stretchr/testify/require"
)

// TestGetBootstrapPolicy_Success
func TestGetBootstrapPolicy_Success(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    // Setup in-memory MCP session (mirroring startInMemoryMCPSession pattern)
    cs, ctx, cleanup := connectQueryTestClient(t)
    defer cleanup()
    
    // Call get_bootstrap_policy
    resp, err := cs.CallTool(ctx, &mcp.CallToolParams{
        Name: "get_bootstrap_policy",
        Arguments: map[string]any{
            "namespace": "production",
        },
    })
    require.NoError(t, err)
    require.False(t, resp.IsError)
    
    // Decode result and verify YAML contains enableDefaultDeny + empty stanzas
    var result struct {
        YAML string `json:"yaml"`
        ClusterVersion string `json:"cluster_version"`
        VersionSource string `json:"version_source"`
    }
    decodeStructured(t, resp.StructuredContent, &result)
    require.NotEmpty(t, result.YAML)
    
    // Unmarshal the YAML to verify structure (same roundtrip test)
    cnp, err := output.UnmarshalPolicy([]byte(result.YAML))
    require.NoError(t, err)
    require.NotNil(t, cnp.Spec.EnableDefaultDeny)
    assert.Empty(t, cnp.Spec.Ingress)
    assert.Empty(t, cnp.Spec.Egress)
}

// TestGetBootstrapPolicy_MissingNamespace
func TestGetBootstrapPolicy_MissingNamespace(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    cs, ctx, cleanup := connectQueryTestClient(t)
    defer cleanup()
    
    // Call with missing namespace
    resp, err := cs.CallTool(ctx, &mcp.CallToolParams{
        Name: "get_bootstrap_policy",
        Arguments: map[string]any{},  // no namespace
    })
    require.NoError(t, err)
    require.True(t, resp.IsError)  // Tool error, not a client error
}
```

---

## Documentation Pattern

### Runbook Markdown

**Analog:** `docs/KNOWN_LIMITATIONS.md`, `README.md` structure (lines 13-37)

**Create as:** `docs/bootstrap-runbook.md` (or similar)

**Structure pattern:**

```markdown
# Bootstrap Policy Runbook

Provides a namespace-level default-deny enforcement artifact without needing to 
observe live traffic — especially useful for greenfield namespaces or cluster 
migration scenarios where you want policies in place *before* traffic starts.

## Prerequisites

- Cilium >= 1.16 (requires enableDefaultDeny field; Phase 22 detects and rejects < 1.16)
- `cpg` binary (v1.7+) OR `kubectl cilium-policy-gen` plugin
- kubeconfig with cluster access (for version detection; offline mode via MCP supported)

## Understanding Default-Deny

A CiliumNetworkPolicy with `enableDefaultDeny: {ingress: true, egress: true}` 
enforces default-deny *on its own* — even with an empty policy set. The empty 
stanzas serve as a regression guard against cilium#35558 (prior versions 
silently no-op'd enableDefaultDeny without explicit stanzas).

## Process (from Cilium's "Creating Policies from Verdicts")

1. **Generate bootstrap artifact**
   ```bash
   cpg bootstrap -n my-namespace > bootstrap.yaml
   ```

2. **Review the artifact** (very simple: enableDefaultDeny + metadata)

3. **Apply to the cluster**
   ```bash
   kubectl apply -f bootstrap.yaml
   ```

4. **Verify enforcement**
   ```bash
   # Traffic is now denied. Observe drops:
   hubble observe -n my-namespace --verdict DROPPED
   
   # Then generate rules from those drops:
   cpg generate -n my-namespace
   ```

5. **Add policies alongside the bootstrap**
   ```bash
   # Generated policies in policies/my-namespace/*.yaml
   # Apply them alongside bootstrap.yaml
   kubectl apply -f policies/my-namespace/
   ```

## Important: policy-audit-mode

**Do NOT use daemon-wide `policy-audit-mode` to "soften" default-deny.** 
Audit mode audits every policy violation cluster-wide and prevents policy 
enforcement — it defeats the purpose of default-deny. If you need to 
debug specific policies in isolation, use per-workload 
`policy.cilium.io/audit-enabled: "true"` annotations instead (Phase 23 feature).

## Via MCP

The `get_bootstrap_policy` MCP tool generates the same artifact:

```json
{
  "namespace": "my-namespace"
}
```

Returns YAML content + detected Cilium version + compatibility warnings.
```

---

## Files with Analogs (Coverage Summary)

| File | Analog | Pattern Class | Status |
|------|--------|---------------|--------|
| `cmd/cpg/bootstrap.go` | replay.go, generate.go | command lifecycle | exact match |
| `pkg/policy/bootstrap_builder.go` | builder.go | struct initialization + marshaling | exact match |
| `cmd/cpg/mcp_tools.go` (add tool) | mcp_query.go | readonly tool registration | exact match |
| Test: enableDefaultDeny roundtrip | builder_test.go | table-driven + YAML roundtrip | exact match |
| Test: README compat pin | readme_compat_test.go | strings.Contains pinning | exact match |
| Test: MCP tool | mcp_query_tools_test.go | in-memory session + tool invocation | exact match |
| `docs/bootstrap-runbook.md` | KNOWN_LIMITATIONS.md + README structure | markdown layout | structural |

---

## Key Conventions to Copy

### Error Wording Style

- **Validation errors** (user input): short, suggestive ("--namespace/-n is required")
- **Preflight warnings** (cluster state): explicit, actionable
  - "version preflight: cluster Cilium version is below one or more feature floors; proceeding without blocking."
- **Below-floor hard errors** (determined version < 1.16): name the floor + version
  - "cluster Cilium version 1.15.0 does not support bootstrap (requires >= 1.16)"

### Stderr Warning Style

- Use `logger.Warn(message, zap.Field...)` (matches generate.go:54, 81, 221)
- Copy exact wording from featureFloors table (COMPAT-02 enforcement)

### Test Helpers

- `decodeStructured(t, structuredContent, &out)` for MCP result parsing
- `connectQueryTestClient(t)` for in-memory MCP sessions
- Table-driven tests for flag validation (`generateFlags.validate()` pattern)

### Flag Naming

- Short flag: `-n`, long flag: `--namespace` (standard Kubernetes style)
- Short flag: `-o`, long flag: `--output` (matches generate.go line 142)
- Help text style: "target namespace (required)" or "output file (default: stdout)"

---

## Metadata

**Analog search scope:** cpg codebase, cmd/cpg + pkg/policy + pkg/k8s + pkg/output directories  
**Files scanned:** 15 Go source files + 1 README  
**Pattern extraction date:** 2026-07-22  
**Phase reference:** 22-bootstrap-artifact-generation / 22-CONTEXT.md

