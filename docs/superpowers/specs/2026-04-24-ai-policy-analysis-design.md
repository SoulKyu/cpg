# AI-Assisted Policy Analysis for `cpg explain`

**Date:** 2026-04-24
**Status:** Draft — pending plan
**Milestone target:** v1.2 (candidate, alongside L7 / `cpg apply` / consolidation / metrics)
**Depends on:** v1.1 (evidence writer, `cpg explain`, `pkg/flowsource`)

## 1. Goal

Extend `cpg explain` with an **optional, opt-in** semantic risk verdict powered by a Large Language Model. For each rule rendered by `explain`, the feature asks an LLM to judge whether the observed connectivity is **plausible for the workloads involved**, producing a risk score, a one-paragraph reasoning, and follow-up questions the operator should ask before applying the policy.

The feature is a **second opinion for human review**, never an authority. It must not change the policies produced by `generate` / `replay`, and it must never block or gate execution of existing commands.

## 2. Non-goals

- Replacing human review or security audits.
- Making `cpg` a runtime threat-detection tool. This is an offline, policy-time overlay, not a live IDS.
- Gating `cpg generate` or `cpg replay` on AI verdicts.
- Running inline during `generate` / `replay` streaming (out of scope; reconsider for v1.3 if demand appears).
- Bundling a pricing table or making cost decisions on the user's behalf (pricing evolves faster than releases).
- Fine-tuning, RAG over internal catalogs, or MCP server exposure (out of scope for v1.2).

## 3. Motivating example

A `cpg replay` run over a staging Hubble capture produces the policy `payments-prod/postgres-primary.yaml` with, among other rules, an egress allow to an external `/32` on TCP/465 (SMTP). Evidence shows 47 flows over 48 hours, all at 03:15 UTC, peer diversity 1, low volume.

`cpg explain payments-prod/postgres-primary` renders the rule and its evidence as today. With `--ai` added, the render carries an additional block:

```
AI Risk: 7/10 — review recommended (confidence 0.72)
Atypical: a primary database initiating outbound SMTP in a regular
03:15 UTC burst. Possibilities in order of likelihood:
  1. A pg_cron job emitting a daily report by email.
  2. Database alerting wired directly to SMTP instead of going through
     an application service.
  3. Low-and-slow exfiltration.
Regular timing and small payload (~1.8 KB) favor 1 or 2.
Open questions:
  - Is there a pg_cron job configured on this instance?
  - Shouldn't this reporting traffic go through app-api?
Model: llama3.1:70b · Cached: false · 680 in / 245 out tokens
```

The operator now has a concrete prompt for the data-platform team before applying the policy — or a concrete reason to reject it.

## 4. User-facing surface

New flags on `cpg explain`, all opt-in. Without `--ai`, behavior is identical to v1.1.

```
cpg explain <NS/WL | policy.yaml> \
  --ai \                                    # enables AI analysis (OFF by default)
  --ai-endpoint <url> \                     # OpenAI-compatible endpoint
  --ai-model <name> \                       # model identifier for that endpoint
  --ai-api-key-env <VAR> \                  # env var holding the API key (empty = no auth, e.g. Ollama)
  --ai-max-rules <N> \                      # deterministic cap; abort pre-flight if exceeded
  --ai-risk-threshold <0-10> \              # hide rules below this score in TEXT output
  --ai-cache / --no-ai-cache \              # local XDG cache; default ON
  --ai-pseudonymize-namespaces \            # SHA-256+salt replacement of ns names in payload
  --no-ai-send-images \                     # skip container image in payload
  --no-ai-send-cidr                         # skip external CIDRs in payload
```

Environment-variable equivalents: `CPG_AI_ENDPOINT`, `CPG_AI_MODEL`, `CPG_AI_API_KEY` (the key itself, for CI), `CPG_AI_MAX_RULES`, `CPG_AI_CACHE_DIR`.

If `--ai` is passed without a resolvable endpoint/model, the command exits with an actionable error that lists two working example configurations (one Ollama-local, one Anthropic-hosted).

## 5. Architecture

New package `pkg/aianalyze`. No change to the generation pipeline (`pkg/hubble`, `pkg/policy`, `pkg/output`, `pkg/evidence`, `pkg/flowsource`, `pkg/diff`).

```
cmd/explain.go
  └── calls pkg/explain.Render(rules, opts) -- existing v1.1
  └── if --ai: calls pkg/aianalyze.Analyze(rules, cfg) BEFORE rendering

pkg/aianalyze/                       (new)
  analyzer.go      // interface Analyzer { Analyze(ctx, Payload) (Verdict, error) }
  openaicompat.go  // OpenAI-compatible HTTP client (Anthropic, Ollama, vLLM, OpenAI, Azure)
  payload.go       // Payload struct + redaction (pseudonymize, drop images/cidr)
  prompt.go        // system prompt + few-shot examples, versioned
  cache.go         // XDG-cached JSON verdicts keyed by hash(payload, model, prompt_version)
  k8s_enrich.go    // client-go lookup for container image + annotations (reuses pkg/k8s kubeconfig)
  preflight.go     // pre-flight rule-count cap + payload redaction preview
  types.go         // Verdict, Payload, Risk, Confidence types shared across commands

pkg/k8s/                             (existing)
  // no change; pkg/aianalyze/k8s_enrich.go reuses LoadKubeconfig + Clientset helpers.
```

`Analyzer` is an interface so the OpenAI-compatible client can be swapped for a test mock. No other backend is implemented in v1.2; the interface leaves room for an Anthropic-native prompt-caching client in a later milestone.

## 6. Data flow

```
cpg explain NS/WL --ai
  ├── 1. Resolve target (existing v1.1) → PolicyRef + []RuleAttribution
  ├── 2. Apply --ingress/--egress/--port/--peer/--peer-cidr/--since filters (existing)
  ├── 3. Pre-flight:
  │       count rules after filters
  │       if count > --ai-max-rules: abort with actionable error, exit 2, zero LLM calls
  ├── 4. For each rule:
  │       a. Build Payload from (rule, evidence stats, optional k8s enrichment, cluster context)
  │       b. Apply redaction flags (pseudonymize, drop images/cidr)
  │       c. Hash normalized payload + model + prompt_version
  │       d. Cache hit: load Verdict, mark cached=true
  │          Cache miss: call Analyzer.Analyze(ctx, payload), write cache
  ├── 5. Filter by --ai-risk-threshold for TEXT output (JSON/YAML keep all)
  └── 6. Render via pkg/explain with the AI block attached to each rule
```

Cache location: `$XDG_CACHE_HOME/cpg/analyses/<model-slug>/<payload-hash>.json`, overridable via `--ai-cache-dir` / `CPG_AI_CACHE_DIR`. Cache is never committed (already out of XDG by convention). Cache invalidates automatically when `prompt_version` or model changes.

## 7. Payload (what leaves the local process)

Per-rule payload, JSON-serialized, sent in a single LLM request per rule:

```json
{
  "rule": {
    "direction": "egress|ingress",
    "src": {
      "namespace": "payments-prod",
      "workload": "postgres-primary",
      "labels": {"app.kubernetes.io/name": "postgres", "...": "..."},
      "image": "postgres:15.2"
    },
    "dst": {
      "type": "endpoint|cidr|entity",
      "namespace": "... (if endpoint)",
      "workload": "... (if endpoint)",
      "labels": {"...": "..."},
      "cidr": "... (if cidr)",
      "entity": "... (if reserved identity)",
      "port": 465,
      "protocol": "TCP"
    }
  },
  "evidence": {
    "flow_count": 47,
    "first_seen": "RFC3339",
    "last_seen": "RFC3339",
    "window": "48h",
    "frequency_pattern": "daily_bursts|regular|bursty|one_shot",
    "burst_times_utc": ["03:15"],
    "peer_diversity": 1,
    "sample_bytes_mean": 1840
  },
  "cluster_context": {
    "other_rules_on_src": ["egress to payments-prod/app-api:5432", "..."],
    "sibling_workloads_same_ns": ["app-api", "worker", "migration-job"]
  }
  // cluster_context is derived from the YAML output directory being explained
  // (other cpg-generated policies in the same namespace). It does NOT query the
  // cluster beyond the single kubectl-equivalent call for the source workload's
  // image. Operators running `cpg explain` offline on a policy dump get the same
  // cluster_context minus the image field.
}
```

Redaction flags:
- `--ai-pseudonymize-namespaces` → `namespace: "ns-a1b2c3"` (HMAC-SHA-256 with a per-host salt stored under `$XDG_CONFIG_HOME/cpg/pseudo-salt`).
- `--no-ai-send-images` → omit `image` field.
- `--no-ai-send-cidr` → replace CIDR with `"0.0.0.0/0 (redacted)"`.

What is **never** sent: individual pod names (aggregated to workload), internal IPs (we send CIDR or workload, never the raw endpoint IP), flow payload contents, HTTP bodies, DNS query contents.

## 8. Verdict (what comes back)

```json
{
  "risk_score": 0,
  "verdict": "safe|review_required|suspicious",
  "reasoning": "one paragraph, <= 400 chars, no chain-of-thought",
  "confidence": 0.0,
  "suggested_questions": ["<= 3 items"],
  "model": "llama3.1:70b",
  "cached": false,
  "tokens": {"input": 0, "output": 0},
  "prompt_version": "v1",
  "schema_version": 1
}
```

`risk_score` is an integer 0–10 (0 = benign, 10 = highly suspicious). `verdict` is derived deterministically in the client: `<4 = safe`, `4-6 = review_required`, `>=7 = suspicious`. We do not trust the model to label itself; we compute the label from the score locally.

`confidence` is a float 0.0–1.0 self-reported by the model. Treated as informational only (displayed, not used to filter).

If the LLM output fails to parse or schema-validate, the client retries once with a stricter "respond with JSON only" preamble. If the retry also fails, the rule is rendered without an AI block, and a warning line is printed at the end of the output listing failed rules. Exit code stays 0.

## 9. Output integration

**Text (default, ANSI on TTY):** an AI block appears after the existing evidence block for each rule whose score ≥ `--ai-risk-threshold`. Color scheme: green (≤3), yellow (4–6), red (≥7). Footer line summarizes model usage:

```
AI summary: 12 rules analyzed, 3 cached, 680+245 tokens avg, model llama3.1:70b
```

**JSON (`--json`) and YAML (`--format yaml`):** each rule object gains an `ai` field matching the Verdict schema above. When `--ai` is not set, the field is omitted. Consumers must treat absence as "not analyzed", not as "safe".

## 10. Error handling

| Condition | Behavior | Exit |
|-----------|----------|------|
| `--ai-max-rules` exceeded pre-flight | Abort before any LLM call, actionable error | 2 |
| Endpoint unreachable / timeout | Rule rendered without AI block, warning appended | 0 |
| HTTP 4xx (auth, bad request) | First rule failure aborts the run with a clear error | 2 |
| HTTP 5xx / connection reset | Retry 1x with backoff, then degrade for that rule | 0 |
| Invalid JSON response | Retry 1x with stricter preamble, then degrade for that rule | 0 |
| Schema mismatch on parsed JSON | Same as invalid JSON | 0 |
| Cache file corrupted | Log warning, ignore cache entry, re-query | 0 |

The AI layer must never crash `cpg explain` on its own. A degraded run is always preferable to a non-zero exit, except for the two pre-flight cases (max-rules, auth/bad-request) where continuing wastes time or tokens.

## 11. Privacy (README section, normative)

The README gains a new section **"AI analysis — read before enabling"**. It states:

- The feature is **off by default**. Nothing network-egresses unless `--ai` is passed.
- With `--ai`, the contents listed in §7 leave the local process and reach the configured endpoint. Operators are responsible for vetting that endpoint against their compliance posture.
- **Recommendation matrix:**
  - Homelab / personal dev clusters: any hosted provider is reasonable.
  - Production with non-sensitive workloads: hosted provider acceptable if the vendor's data-handling terms are acceptable to the operator.
  - Regulated workloads (payments, health, PII, defense, government): **self-hosted only** (Ollama / vLLM / LiteLLM). Document a reference setup in the README.
  - Air-gapped environments: do not enable, or use a fully-local model with offline weights.
- **The model may hallucinate.** The verdict is a second opinion, not an authority. No automation should branch on it without a human in the loop.
- **Cost varies by provider and moves fast.** The README does not list prices. Operators check their provider's current pricing before running.

## 12. Testing

All tests live under the new `pkg/aianalyze`. No change to existing test suites beyond a small addition to `cmd/explain_test.go` covering flag wiring.

**Unit:**
- Payload construction from a fixed rule + evidence fixture.
- Redaction: pseudonymize, image strip, CIDR strip.
- Cache: hit, miss, corrupted entry, prompt-version bump invalidates.
- Verdict parsing: valid JSON, retry on invalid, retry on schema mismatch, final degradation.
- Verdict label derivation (score → safe / review_required / suspicious).
- Pre-flight: `--ai-max-rules` exceeded aborts with zero LLM calls.
- K8s enrichment: image lookup happy path, pod-not-found degrades to no image.

**Integration (httptest):**
- Mock OpenAI-compatible server returning canned verdicts for 10 table-driven scenarios (postgres→smtp, frontend→dns, job→s3, random-pod→payment-db, reserved:world, etc.).
- Verify exact payload structure sent to the server (after redaction).
- Verify text / JSON / YAML rendering for each scenario.
- Graceful degradation on timeouts and 5xx.

**Not tested:**
- Against real hosted LLMs (non-deterministic, cost, CI key management).
- Against a real Ollama instance (covered manually in the README's reference setup).

## 13. Phase breakdown (candidate v1.2)

- **Phase 7 — AI analyzer core.** `pkg/aianalyze` with `Analyzer` interface, OpenAI-compat client, Payload, redaction, cache, pre-flight, prompt. Unit + httptest coverage. No CLI wiring.
- **Phase 8 — `cpg explain --ai` integration.** Flag wiring, k8s image enrichment, text / JSON / YAML rendering with the AI block, README privacy section with reference Ollama setup. Integration tests.

Phase 7 is independently testable and merges before Phase 8. Phase 8 depends on Phase 7 and on the existing v1.1 `cpg explain` renderer. Neither phase depends on other v1.2 candidates (L7, `apply`, consolidation, metrics), so the AI pair can ship even if the rest of v1.2 slips.

## 14. Open questions

- **Prompt versioning cadence.** `prompt_version` busts the cache. How do we test that a prompt change is actually an improvement? Out of scope for v1.2 — we ship `v1` and iterate in issues.
- **Multi-language reasoning.** The prompt is English-only. Enough models handle that fluently, but non-English operators may want French / Spanish / Japanese reasoning. Not shipping language selection in v1.2; revisit if users ask.
- **Batch mode.** One LLM request per rule is simple and resumable via cache. A batch-per-policy call would cut latency but complicates error handling and retry. Deferred until someone demonstrates a policy with >50 rules routinely analyzed.

## 15. Acceptance criteria

- `cpg explain NS/WL` without `--ai` behaves identically to v1.1 (bit-for-bit output where feasible; no network imports linked in the binary for that path — if the LLM SDK gets pulled in, the feature is still correctly gated at runtime).
- `cpg explain NS/WL --ai` with a working endpoint produces an AI block for each rendered rule, or a clear warning line for each rule where analysis failed.
- Cache hit on an identical payload + model + prompt_version costs zero LLM tokens and returns the prior verdict.
- `--ai-max-rules` aborts before any network call when exceeded.
- With `--ai-pseudonymize-namespaces` / `--no-ai-send-images` / `--no-ai-send-cidr`, the wire payload observably omits or redacts the corresponding fields (verified by httptest integration).
- The README privacy section exists and links a reference Ollama setup.
- All tests in §12 pass.

---

*Design authored 2026-04-24 as a v1.2 candidate. Supersedes nothing. Linked from `.planning/PROJECT.md` "Current Milestone" once v1.2 is scoped via `/gsd:new-milestone`.*
