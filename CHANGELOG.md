# Changelog

## [1.11.0](https://github.com/SoulKyu/cpg/compare/v1.10.0...v1.11.0) (2026-07-23)


### Features

* **20-01:** add includeAudit gate field and AUDIT diagnostic counter ([43adde7](https://github.com/SoulKyu/cpg/commit/43adde77adee612929c7965219d1c6b107a5879e))
* **20-01:** widen classification gate to include AUDIT verdicts ([5b586ae](https://github.com/SoulKyu/cpg/commit/5b586aea32d938988653f698136a4e404e44096f))
* **20-02:** widen FlowSource interface + filter sites 1-4, thread IncludeAudit through pipeline ([dceb3ad](https://github.com/SoulKyu/cpg/commit/dceb3adb3177e7c1771e39098a0a4d924b2c06a2))
* **20-04:** register --include-audit CLI flag on generate/replay ([4e397d1](https://github.com/SoulKyu/cpg/commit/4e397d1c35be6bacd7e3c941685bbf909be9372e))
* **20-04:** thread include_audit MCP arg through StartArgs to PipelineConfig ([e49e93d](https://github.com/SoulKyu/cpg/commit/e49e93d314a910f1e3e6612845de6d2902fb2dd0))
* **21-01:** add pkg/k8s/version.go Cilium version detection ([cfa36ea](https://github.com/SoulKyu/cpg/commit/cfa36ead53194068bc16650773ea2b843e5c23e7))
* **21-03:** add maybeRunVersionPreflight always-on preflight in generate.go ([6362e89](https://github.com/SoulKyu/cpg/commit/6362e89d5d70240d7ebb91d8c923c202316b1336))
* **21-04:** add compat fields + detectVersionFn seam + resolveSetup wiring ([3ca6465](https://github.com/SoulKyu/cpg/commit/3ca6465dfae0f12611f5769967fd01665dd0f4d5))
* **22-01:** add BuildBootstrapPolicy default-deny CNP constructor ([2a1b0fc](https://github.com/SoulKyu/cpg/commit/2a1b0fcb18759953a0e2d5f48a8e5121a18d9950))
* **22-02:** add cpg bootstrap CLI command + version gate ([52861d7](https://github.com/SoulKyu/cpg/commit/52861d75bc216ecad3bd3c8857e3e6a110bd35ae))
* **22-02:** add readonly get_bootstrap_policy MCP tool ([87630fd](https://github.com/SoulKyu/cpg/commit/87630fd7ba2f95d395abfb60de939311280be8c4))
* **23-01:** add read-before-flip, canonical flip, binary gate, daemon precondition ([f0ab85f](https://github.com/SoulKyu/cpg/commit/f0ab85f72e522bd7d255fa3ab5424e57a7e3b8b6))
* **23-01:** add SPDY exec primitive + node-to-agent-pod mapping ([c36e03e](https://github.com/SoulKyu/cpg/commit/c36e03e81eb39e1a8d389f087affbc1788d906cc))
* **23-02:** add auditwindow.Manager scaffold + Open precondition/flip/UID bookkeeping ([f2de4da](https://github.com/SoulKyu/cpg/commit/f2de4dae86cb54d718e2305114fefe34ae7efd02))
* **23-02:** add bounded revert fan-out for Close/Shutdown with per-endpoint reporting ([4c9c66e](https://github.com/SoulKyu/cpg/commit/4c9c66e23b22efff0df8f57721465bf461cb508f))
* **23-02:** add new-endpoint watcher with bounded-backoff reconnect ([42b3b8f](https://github.com/SoulKyu/cpg/commit/42b3b8f8d6e818697b39947c69001e5a88163659))
* **23:** add cpg audit-window command ([6b7157a](https://github.com/SoulKyu/cpg/commit/6b7157a24692a0bbab8bd42b7b76bbe5bf0e97ca))
* **24:** add cpg-audit-onboard + cpg-policy-review skills ([de97afa](https://github.com/SoulKyu/cpg/commit/de97afa1168ac87812eb73e79f8d46e1a28befab))
* **24:** add cpg-health-report + cpg-mcp-smoke skills + README agent tooling ([26d06a5](https://github.com/SoulKyu/cpg/commit/26d06a5032b6a5bdf78627d5ead4c695adff1962))
* **24:** add cpg-operator agent + cpg-triage skill ([817db1f](https://github.com/SoulKyu/cpg/commit/817db1f5a9cbee18b9927b246903fa5343668884))
* **quick:** use WebSocket exec with SPDY fallback in audit-window exec path ([2465740](https://github.com/SoulKyu/cpg/commit/24657403b6191dd3ec771e0b7d6365438e7884fe))


### Bug Fixes

* **20:** surface AuditVerdictCount through SessionStats and StopResult (WR-01) ([ada7b30](https://github.com/SoulKyu/cpg/commit/ada7b30f1fd17c2360756751037b180104b31e2b))
* **21:** WR-01 bound CLI version preflight and suppress Ctrl+C warn ([c4db965](https://github.com/SoulKyu/cpg/commit/c4db965e763a23f425884e9d2937e10cc23eca41))
* **21:** WR-02 bound GetNodes RPC with versionDetectTimeout budget ([0144ae0](https://github.com/SoulKyu/cpg/commit/0144ae06cfe9279dbc46fdb802baa9762b3bf2ac))
* **22-02:** make cpg bootstrap stdout-only, restoring zero new SEC-01 allowlist entries ([61c58c0](https://github.com/SoulKyu/cpg/commit/61c58c0d4fa1ff635cd2c1cb0c9da0d67890002d))
* **22-02:** update pinned MCP tool-count tests for get_bootstrap_policy ([a111188](https://github.com/SoulKyu/cpg/commit/a1111885fe04948a4ef9ad2961fc83de373cc2b0))
* **22:** review fixes — runbook -o drift + DNS-1123 namespace validation on both surfaces ([4ebdfe4](https://github.com/SoulKyu/cpg/commit/4ebdfe4eabae96f80b2b50d25b3e47c9f9b17e9a))
* **23:** CR-01/CR-02/WR-01 route audit-window revert through bounded Shutdown ([4d62126](https://github.com/SoulKyu/cpg/commit/4d621264a03324e7e21547f598508823996e458c))
* **23:** report possibly-stuck endpoints when revert deadline expires ([acee4a9](https://github.com/SoulKyu/cpg/commit/acee4a9b4a5bfffa24b7fd34d958b12570a66e63))
* **23:** WR-02/WR-03 harden SEC-01 exec tripwire negative half ([dc7f21c](https://github.com/SoulKyu/cpg/commit/dc7f21c442f7089e0ec3d4ad3c6bf080ebe99680))
* **24:** correct nonexistent 'cpg explain --output json' flag to --json (skill + tool descriptions) ([d011d16](https://github.com/SoulKyu/cpg/commit/d011d166ee40c9f8d8b134c95f976abc0b2670fa))
* **24:** correct remaining --output json reference in README MCP table ([755cf0c](https://github.com/SoulKyu/cpg/commit/755cf0c8b779dd92a02b0ba1bf4b5532b9535f2e))
* resolve new lint findings (errcheck Fprintln, QF1008 embedded ObjectMeta, QF1001 De Morgan) ([86fecf0](https://github.com/SoulKyu/cpg/commit/86fecf0d0fa91136c2edf23a1397fd23a48edebf))

## [1.10.0](https://github.com/SoulKyu/cpg/compare/v1.9.1...v1.10.0) (2026-07-22)


### Features

* **16-01:** atomic temp+rename write in pkg/output/writer.go ([55f7cc2](https://github.com/SoulKyu/cpg/commit/55f7cc20513919632e7fa2fc2c1f222c967d4b59))
* **16-02:** pin github.com/modelcontextprotocol/go-sdk v1.6.1 ([09d9e96](https://github.com/SoulKyu/cpg/commit/09d9e964d5923a59ddc715039b0f2ec5483b1f7b))
* **16-03:** create cpg mcp server skeleton and register command ([93e7b3e](https://github.com/SoulKyu/cpg/commit/93e7b3e8e6e31b50e4e61d4a7b3a3168c36a3277))
* **17-01:** add nil-safe OnFinal hook to PipelineConfig ([507d0b3](https://github.com/SoulKyu/cpg/commit/507d0b399eb5134bb7772728f1310bb79c42a824))
* **17-02:** add pkg/session pipeline config builder + crash guard ([c3b233e](https://github.com/SoulKyu/cpg/commit/c3b233e3f59c8d4b61216d523766bf9b9ad104f9))
* **17-02:** add pkg/session state model + result shapes ([5fd92f7](https://github.com/SoulKyu/cpg/commit/5fd92f799ef04db59588335c20d52fa1c7190ad2))
* **17-03:** create pkg/session/manager.go single-slot state machine ([6674d8b](https://github.com/SoulKyu/cpg/commit/6674d8b934622894094b32e6410de8704e0fcd20))
* **17-04:** register session MCP tools and wire Manager into runMCPServer ([83f70c0](https://github.com/SoulKyu/cpg/commit/83f70c0e853d00a0137d40f8fce39af39af8965e))
* **17-05:** add terminal-error data layer to pkg/session ([4bf0063](https://github.com/SoulKyu/cpg/commit/4bf00633d3379e44295050468ad6c48e298c29aa))
* **17-05:** autonomously transition State on genuine pipeline failure ([e0ce76d](https://github.com/SoulKyu/cpg/commit/e0ce76d766c0f28679877421db55f4011095e6be))
* **17-06:** bound MCP-supplied timeout/flush_interval above 24h (WR-03) ([bf3012c](https://github.com/SoulKyu/cpg/commit/bf3012c3c5e0fd359db9346f59fe5bace1a74b61))
* **17-06:** merge sessionCtx into setupCtx so Shutdown aborts mid-setup (WR-02) ([f8f9bdd](https://github.com/SoulKyu/cpg/commit/f8f9bdd7421930b52686ec1bf38f1e0291c4d431))
* **17-08:** classify genuine crash on sessionCtx cancellation, not error identity (WR-01) ([cca60a1](https://github.com/SoulKyu/cpg/commit/cca60a1411bb5bda0f69de22c0021ad4467226d3))
* **17-08:** track explicit-stop separately from State via atomic Swap (WR-02) ([0159b66](https://github.com/SoulKyu/cpg/commit/0159b660da6c552f7a98f8ef15a0804cf7bd99eb))
* **17-09:** broaden autonomous-exit guard to fire on clean nil drain ([d4fdbb1](https://github.com/SoulKyu/cpg/commit/d4fdbb123c685bb87483ac84f23c17527edf668f))
* **18-01:** implement pkg/explain with exported Filter/Output/Render* (GREEN) ([667f5ba](https://github.com/SoulKyu/cpg/commit/667f5ba82a5e59884d2cedbb4515f89511bb1fca))
* **18-01:** thin cmd/cpg/explain.go to call pkg/explain (GREEN) ([ca5b920](https://github.com/SoulKyu/cpg/commit/ca5b920b0f9ff9de53901e027bf8c24a942da1d9))
* **18-02:** export cluster-health types and add hubble.ReadClusterHealth ([7dabea1](https://github.com/SoulKyu/cpg/commit/7dabea16ab3446b4844565a8d9b8d3dce87d3f31))
* **18-02:** export output.ReadPolicyFile with wrapped-not-exist contract ([7ace14c](https://github.com/SoulKyu/cpg/commit/7ace14cb8c77a0e9ec32cf1fe3e15a1b414c454d))
* **18-03:** implement get_cluster_health with D-13 3-way branch ([1576330](https://github.com/SoulKyu/cpg/commit/157633035f5062e4f4c7b111d8f0d06d9fd598a8))
* **18-03:** implement list_policies and get_policy query tools ([ee6a6c1](https://github.com/SoulKyu/cpg/commit/ee6a6c16330cc8ab808d73f4b063b252f515b01b))
* **18-04:** implement get_evidence — paginated pkg/explain evidence (QRY-03) ([dd6d39f](https://github.com/SoulKyu/cpg/commit/dd6d39fe1e5e62879d9f93f910e6bb9ecf0e1b72))
* **18-04:** implement pagination + enum-schema infra (mustQuerySchema, cursor, paginate) ([2b722b0](https://github.com/SoulKyu/cpg/commit/2b722b03c2fae9d69ab8950c1140fea5e381c8c2))
* **18-05:** implement list_dropped_flows composed view (QRY-01) ([9836802](https://github.com/SoulKyu/cpg/commit/9836802f19455dd613d214072b1bb8bca91393ac))
* **19-01:** SEC-01 RTA reachability + direct-call-scan audit test ([258decc](https://github.com/SoulKyu/cpg/commit/258decc109851d381e770b19cc7cb4a46333477b))
* **19-02:** add graceful e2e lifecycle test + SRV-01 handshake/schema/annotation assertions ([cade360](https://github.com/SoulKyu/cpg/commit/cade36080d055e0cdd7160eb4274a6157857f155))
* **19-02:** build e2e infra - fake Hubble relay, -race build helper, subprocess/tee harness, drop-flow fixtures ([2648732](https://github.com/SoulKyu/cpg/commit/2648732c49b524fd09fd4ed894ca3a8b92bcf4b5))
* **19-04:** add ungraceful-disconnect e2e variant (SRV-04/D-08/D-09) ([140d25f](https://github.com/SoulKyu/cpg/commit/140d25ff897404fb11e8a775dd05bdfa570e2360))
* **v1.5:** MCP integration — readonly stdio server (phases 16-19) ([81ebf2c](https://github.com/SoulKyu/cpg/commit/81ebf2c4c5b8b48c9a24d7726567bcd674ce2e93))


### Bug Fixes

* **16:** guard seam-audit identity assertion against test2json stderr aliasing ([ab665c5](https://github.com/SoulKyu/cpg/commit/ab665c5c32379f44114a92e2c1838f99345bfc25))
* **16:** make seam-audit test immune to global stdout swaps ([2cf0754](https://github.com/SoulKyu/cpg/commit/2cf0754f9cd70233a16b0b3f734ff0d6b6a272f1))
* **16:** WR-01 check cleanup error returns in atomic write path ([2389f2f](https://github.com/SoulKyu/cpg/commit/2389f2fbbcabab3315c158d8d0ec8a6f917cc690))
* **16:** WR-02 stop swallowing cpg mcp startup/runtime errors ([4fb7ba2](https://github.com/SoulKyu/cpg/commit/4fb7ba294053c2da8e1dc71069257adc41544b64))
* **18:** WR-01 classify DROP_REASON_UNKNOWN evidence samples as unknown, not transient ([8ea5a42](https://github.com/SoulKyu/cpg/commit/8ea5a425c25851e3f6fc906aef8ea466ea489013))
* **18:** WR-02 paginate list_policies and cap get_cluster_health breakdown maps ([afab493](https://github.com/SoulKyu/cpg/commit/afab4931bbe7fd7f2371507074c565bc163ccdeb))
* **18:** WR-03 read policy file once in get_policy to avoid metadata/YAML skew ([794175a](https://github.com/SoulKyu/cpg/commit/794175ad649a764274e734762586530c5239502e))
* **18:** WR-04 add session.DeriveSessionPaths as single source of truth for output layout ([d733cbb](https://github.com/SoulKyu/cpg/commit/d733cbb1f0164fdaf8fd86ad72c429a38fd820b9))
* **19:** address plan-checker findings (dropclass scope, k8s-verb heuristic) ([356e50c](https://github.com/SoulKyu/cpg/commit/356e50c8cad7f6d952974d2996044bd04750ea32))
* **19:** correct D-10 dropclass scope in context/research/patterns + close out validation strategy ([a6ab6ab](https://github.com/SoulKyu/cpg/commit/a6ab6ab9e26de49ee7119799b7fdd0033c7bcde6))
* **19:** IN-01 acknowledge func-value call dispatch is unchecked by Stage 3 scan ([8c9e6f5](https://github.com/SoulKyu/cpg/commit/8c9e6f5c6455dcca483963b609563ca5cb8ea83b))
* **19:** IN-02 soften README exec-plugin timeout claim, note kubeconfig-load exception ([f0d706e](https://github.com/SoulKyu/cpg/commit/f0d706ea80b50860bdcb02fbf3387350901c63df))
* **19:** WR-01 replace tautological BFS self-check with real reachability assertions ([7665120](https://github.com/SoulKyu/cpg/commit/7665120bced9750a9a21e2c252d9e215a42267e2))
* **19:** WR-02 extend disallowedFSWrite watchlist with metadata/link/truncate os.* mutators ([37dbea4](https://github.com/SoulKyu/cpg/commit/37dbea4b05b7860cab81694c39511bad1a62b00f))
* **19:** WR-03 add DeleteCollection/UpdateStatus/ApplyStatus to k8sWriteVerbs ([e92669f](https://github.com/SoulKyu/cpg/commit/e92669f4b9859b0001694c1d472346b93af3b2c9))
* **19:** WR-04 add t.Cleanup to guarantee e2e subprocess termination ([5b5d5c7](https://github.com/SoulKyu/cpg/commit/5b5d5c76e82f4ca48dd39e4266fca2e363eed143))
* **deps:** bump golang.org/x/text to v0.39.0 (GO-2026-5970) ([bbce104](https://github.com/SoulKyu/cpg/commit/bbce1043053b6dbb23d41baae995c5d117636456))

## [1.9.1](https://github.com/SoulKyu/cpg/compare/v1.9.0...v1.9.1) (2026-07-20)


### Bug Fixes

* **ci:** gate lint on new issues only until v1.5 debt cleanup ([516d77b](https://github.com/SoulKyu/cpg/commit/516d77b490acf7fef7d081b0d15a77dce43934fa))
* **ci:** pin Go toolchain to 1.25.12 for patched stdlib (govulncheck) ([ab97d96](https://github.com/SoulKyu/cpg/commit/ab97d964714b90722c1f34357d00cd097562a736))
* **ci:** repin actions/checkout to the v4.3.1 release SHA ([f802e79](https://github.com/SoulKyu/cpg/commit/f802e79250fb3f8fb01a9f734d1514335a15ddf6))
* **ci:** run pipeline on master and pin actions/tools to immutable versions ([c4cad81](https://github.com/SoulKyu/cpg/commit/c4cad81a40a49b6a1390ce620138c4f4ce51bd1c))
* **cli:** emit FILTER-03 warning once, guard empty Direction render, use json struct tags ([85d1796](https://github.com/SoulKyu/cpg/commit/85d1796285953870b741a392b657f73ad04f6e26))
* **deps:** bump cilium to v1.19.4 and x/net to v0.55.0 (govulncheck) ([37a7fb4](https://github.com/SoulKyu/cpg/commit/37a7fb4b3425bea410476358ffca7357c255e9d2))
* **evidence,output:** dedup contributing sessions, validate policy refs, describe all selectors ([69840d4](https://github.com/SoulKyu/cpg/commit/69840d4a576fb6de837f3c063e1ce7e582ab9342))
* **flowsource,dropclass:** report truncated replay, honor ctx cancel in scan loop, hints doc ([6843471](https://github.com/SoulKyu/cpg/commit/684347103466cb2053c823ee668e3bfa7deb58ae))
* **hubble:** surface stream failures, populate LostEvents, apply --timeout, count write failures ([1559343](https://github.com/SoulKyu/cpg/commit/15593433f79e3e6ec83f75b795d22a605f4e0686))
* land all 29 confirmed findings from the Fable 5 full code review ([be06b7b](https://github.com/SoulKyu/cpg/commit/be06b7b45ef8922f727ea513f929e1f3c5e2a1bd))
* **output:** validate policy ref before writing YAML ([7e4ce88](https://github.com/SoulKyu/cpg/commit/7e4ce88da47b4cad455112f54f17c7797b7e965c))
* **policy:** nil-Spec merge guard, content-aware dedup keys, multi-entry ICMP merge, DNS rule dedup ([4c2e474](https://github.com/SoulKyu/cpg/commit/4c2e474c2e43fbed752839c0e43004769fbe5f04))

## [1.9.0](https://github.com/SoulKyu/cpg/compare/v1.8.0...v1.9.0) (2026-04-27)


### Features

* **10-01:** implement drop-reason classifier, hints map, version ([10bf710](https://github.com/SoulKyu/cpg/commit/10bf71035c4a6719f3f12f291a44ca3541d75ac5))
* **10-02:** add SetWarnLogger + dedup WARN for unrecognized drop reasons ([b1ab014](https://github.com/SoulKyu/cpg/commit/b1ab01456a2d0a119093c48a1b8561d1069b0f36))
* **11-01:** implement classification gate in Aggregator.Run() ([3def91e](https://github.com/SoulKyu/cpg/commit/3def91e4f042285225f9e95dceb50c37f5a082e8))
* **11-02:** implement healthWriter + wire healthCh in pipeline (GREEN) ([5a2b838](https://github.com/SoulKyu/cpg/commit/5a2b83852cb3f05322348e54dbd61a88aca4e1ed))
* **12-01:** implement PrintClusterHealthSummary + healthWriter.Snapshot() ([5fd943e](https://github.com/SoulKyu/cpg/commit/5fd943e83f3c85ae5d5274d2847e9b9c3d3ace81))
* **13-01:** add ignore-drop-reason filter to aggregator ([5c96669](https://github.com/SoulKyu/cpg/commit/5c9666958ad787a380ececa179f50b4fe9177e1f))
* **13-02:** wire --ignore-drop-reason and --fail-on-infra-drops flags ([c29233a](https://github.com/SoulKyu/cpg/commit/c29233ac157ff0dc87b07c615958bcd93af62bec))
* **13-03:** ExitCodeError + --fail-on-infra-drops exit logic ([296a851](https://github.com/SoulKyu/cpg/commit/296a8516ff01e27f41af5c2bef2b8a7e07723755))
* **quick-260427-aml:** PreRunE validation + Levenshtein top-5 suggestions + ,ok lookup (I2+I3+I7) ([1e5f398](https://github.com/SoulKyu/cpg/commit/1e5f3984fee4f95cefff80ef5800daf53ff93eba))


### Bug Fixes

* **quick-260427-aml:** non-blocking healthCh send + fallback snapshot under --no-evidence (C1+C2) ([3ef8573](https://github.com/SoulKyu/cpg/commit/3ef8573898baa123b5e0626a7b1da1349403a8ad))
* **quick-260427-aml:** omit generic-URL hints + README timeout --preserve-status (M1+M2) ([281f685](https://github.com/SoulKyu/cpg/commit/281f6853ec0929a36228008ad4f606b3edeefe26))
* **quick-260427-aml:** summary topN tie boundary + adaptive width + Transient fixture (I8+M5+M6) ([4107d25](https://github.com/SoulKyu/cpg/commit/4107d25f17ae3a3f1e2964a135503fb6b9d4a660))
* **quick-260427-aml:** SummaryPathState enum + DropClass.String() source of truth (C3+I1+I5 partial) ([71c1b50](https://github.com/SoulKyu/cpg/commit/71c1b5002948e0cfebcf4e90cc58e25f3acf6cfd))
* **quick-bp7:** Levenshtein hardening — runes + threshold + sort.Slice + conditional suggestions (I-2/I-3/I-4/I-5) ([960d3cd](https://github.com/SoulKyu/cpg/commit/960d3cded7e688949b398df1c3aa0fd875cb8db0))
* **quick-bp7:** Snapshot returns deep-independent copies + Remediation omitempty doc (C-2/I-9) ([d76a2e6](https://github.com/SoulKyu/cpg/commit/d76a2e684e60a7df209e99320f9d42f39d49181b))

## [1.8.0](https://github.com/SoulKyu/cpg/compare/v1.7.0...v1.8.0) (2026-04-26)


### Features

* **pa5:** --ignore-protocol drops flows by L4 proto in aggregator + counter ([6cea8d5](https://github.com/SoulKyu/cpg/commit/6cea8d5feeeda1547807468dca9597c70059a883))

## [1.7.0](https://github.com/SoulKyu/cpg/compare/v1.6.0...v1.7.0) (2026-04-25)


### Features

* **08-01:** implement HTTP L7 extraction primitives ([70a3e97](https://github.com/SoulKyu/cpg/commit/70a3e97d374182babb56f9eb31976e8a1aba9044))
* **08-02:** wire HTTP L7 codegen into BuildPolicy (HTTP-01, HTTP-04) ([d3724cb](https://github.com/SoulKyu/cpg/commit/d3724cbf364a5569615aee6f55d6ec5a2fe1c31b))
* **08-03:** pipeline L7 codegen + VIS-01 warning + evidence L7Ref ([92eded4](https://github.com/SoulKyu/cpg/commit/92eded418797110bcfe0a06aa6600dc1c7373af4))
* **cli:** plumb --l7 + --no-l7-preflight (no-op codegen, Phase 8 lights up) ([3d7bee9](https://github.com/SoulKyu/cpg/commit/3d7bee94bd0de236ee2f465552f62e50ac2f72de))
* **evidence:** bump schema v1 to v2 with optional L7Ref (EVID2-01) ([2389793](https://github.com/SoulKyu/cpg/commit/2389793f1d7b88b319c0769c9ecc35cba51e141e))
* **explain:** --http-method, --http-path, --dns-pattern filters (L7CLI-02) ([546af15](https://github.com/SoulKyu/cpg/commit/546af15a2700036cbacc0ace95f3445ff63dcec6))
* **explain:** render L7 attribution in text/JSON/YAML (L7CLI-03) ([595b584](https://github.com/SoulKyu/cpg/commit/595b5846ed72a32c0871447d81bd8cde70101a1e))
* **hubble:** aggregator DNS counter + evidence DNS branch (DNS-01, VIS-01 gate) ([fef0971](https://github.com/SoulKyu/cpg/commit/fef0971483302421073ea37e6477c553a758296d))
* **k8s:** L7 preflight checks for cilium-config + cilium-envoy with warn-and-proceed (VIS-04, VIS-05) ([5ab5556](https://github.com/SoulKyu/cpg/commit/5ab5556e472e0f6cdcccfa2a85f1a93101e581b2))
* **policy:** extractDNSQuery + kube-dns companion injector (DNS-01, DNS-02) ([1013f6f](https://github.com/SoulKyu/cpg/commit/1013f6f268158e7eefb4f14e2f6e1dbad835f1d0))
* **policy:** wire DNS L7 codegen into BuildPolicy with companion injector (DNS-01, DNS-02, DNS-03) ([a71f908](https://github.com/SoulKyu/cpg/commit/a71f90821b1ae1c6df99f5fbe156960745665ef4))


### Bug Fixes

* **policy:** preserve Rules in mergePortRules + sort L7 lists in normalizeRule + L7 discriminator on RuleKey (EVID2-02, EVID2-03, EVID2-04) ([615e527](https://github.com/SoulKyu/cpg/commit/615e527df413a7a74cc2b59ef61631b8e3284796))

## [1.6.0](https://github.com/SoulKyu/cpg/compare/v1.5.1...v1.6.0) (2026-04-24)


### Features

* **cli:** cpg replay + cpg explain subcommands with dry-run + evidence ([73e0e4b](https://github.com/SoulKyu/cpg/commit/73e0e4b43f32461ee9222c9abb6efddd64ac63e0))
* **evidence:** add JSON schema for per-rule attribution ([db6f62f](https://github.com/SoulKyu/cpg/commit/db6f62f851788ce1aa6f9cd848c840d49e644911))
* **evidence:** atomic writer + reader with schema version check ([a885ccc](https://github.com/SoulKyu/cpg/commit/a885ccc463bed1152ce372b1c7b34a31ab4d07f8))
* **evidence:** merge semantics with FIFO sample/session caps ([259de0f](https://github.com/SoulKyu/cpg/commit/259de0f50c6f247097173ee81c28b46687f78178))
* **evidence:** XDG-aware path resolver with output-dir hash ([2c6a58e](https://github.com/SoulKyu/cpg/commit/2c6a58e0ab6f3f0e8bf005feba0a1fa2ab762c92))
* **flowsource:** jsonpb file source with DROPPED filter, gzip, error counters ([79c1d96](https://github.com/SoulKyu/cpg/commit/79c1d9660fa8803354073fe7788d0315d1f2be54))
* **hubble,diff:** dry-run mode with unified YAML diff ([ab4cb0e](https://github.com/SoulKyu/cpg/commit/ab4cb0e0ce7bff791b21bac9d32eadbd9879b774))
* **hubble,evidence:** evidence writer goroutine + pipeline fan-out ([05c8320](https://github.com/SoulKyu/cpg/commit/05c8320fb4000798066293895019dee10dc027ba))
* **policy:** add RuleKey and RuleAttribution types ([3e00803](https://github.com/SoulKyu/cpg/commit/3e0080382a36ed46d5efcaba3debc70d4bb9cb86))
* **policy:** BuildPolicy returns per-rule attribution ([34eb318](https://github.com/SoulKyu/cpg/commit/34eb3185d5fd98acfdf72216a85bd1ce07eb50f4))

## [1.5.1](https://github.com/SoulKyu/cpg/compare/v1.5.0...v1.5.1) (2026-04-24)


### Miscellaneous Chores

* release 1.5.1 ([95572e1](https://github.com/SoulKyu/cpg/commit/95572e1f08790e102f53268bc0bea69b35aa055e))

## [1.5.0](https://github.com/SoulKyu/cpg/compare/v1.4.0...v1.5.0) (2026-03-11)


### Features

* add Flush() with structured INFO summary and counter reset ([53953e1](https://github.com/SoulKyu/cpg/commit/53953e14a042bdff044aa508edc033befc487652))
* add UnhandledTracker with Track() and dedup logic ([91ff19d](https://github.com/SoulKyu/cpg/commit/91ff19d7513bf2e6b00497d01bf18e7b8976135e))
* flush UnhandledTracker at each aggregation cycle and shutdown ([8a6742e](https://github.com/SoulKyu/cpg/commit/8a6742e9809f58cd72edf828743eb76c6aed6525))
* integrate UnhandledTracker into aggregator for nil_endpoint and empty_namespace ([e5bc231](https://github.com/SoulKyu/cpg/commit/e5bc2316eefc02cff8ee41589f82e8cf7f39b9cb))
* integrate UnhandledTracker into policy builder for all skip reasons ([a4012b9](https://github.com/SoulKyu/cpg/commit/a4012b98a0163b1ff75e706df794cb20b536f730))


### Bug Fixes

* add TrafficDirection to dedup key, use strings.Contains in tests ([12373e9](https://github.com/SoulKyu/cpg/commit/12373e99a8838c23c7d45b795d3b66105998f92f))

## [1.4.0](https://github.com/SoulKyu/cpg/compare/v1.3.0...v1.4.0) (2026-03-10)


### Features

* respect 50 char max ([fdd746e](https://github.com/SoulKyu/cpg/commit/fdd746ebb890221c22228c6113f338c69095b844))

## [1.3.0](https://github.com/SoulKyu/cpg/compare/v1.2.0...v1.3.0) (2026-03-10)


### Features

* add kubectl krew plugin support (cilium-policy-gen) ([9c03bbf](https://github.com/SoulKyu/cpg/commit/9c03bbf790c331223dfd82d7f3f1c66e47942305))


### Bug Fixes

* remove invalid .Contributors template and fix deprecated archives.format ([6cde130](https://github.com/SoulKyu/cpg/commit/6cde130af69b7fa86ea739e2cd5674ecd4a42127))

## [1.2.0](https://github.com/SoulKyu/cpg/compare/v1.1.0...v1.2.0) (2026-03-09)


### Features

* annotate generated policy rules with human-readable comments ([4a9b2d9](https://github.com/SoulKyu/cpg/commit/4a9b2d9bead6bca3e129569bee224527407fdac3))


### Bug Fixes

* align go module path with GitHub repository (github.com/SoulKyu/cpg) ([d9116a3](https://github.com/SoulKyu/cpg/commit/d9116a3ac5cf35cd744dd0007756842f20ee9d6f))

## [1.1.0](https://github.com/SoulKyu/cpg/compare/v1.0.3...v1.1.0) (2026-03-09)


### Features

* support ICMP flows and reserved entities in policy generation ([aa252ef](https://github.com/SoulKyu/cpg/commit/aa252ef17b57f9bc265f119ff4c3dec217e36d62))


### Bug Fixes

* deduplicate reserved identity warnings to log once per identity ([25d316a](https://github.com/SoulKyu/cpg/commit/25d316a1a78f606fec97b6349f4b76774a4aaf2c))
* improve label selection with component priority and Cilium label filtering ([703a751](https://github.com/SoulKyu/cpg/commit/703a751f45de305779d18a24a11cce6ccb4991a9))
* only warn on actionable reserved identities, demote unknown to debug ([908d08d](https://github.com/SoulKyu/cpg/commit/908d08df64b57fab4bd4110944f4ffea98b19530))
* split ICMPs and ToPorts into separate rules per Cilium spec ([a698e9f](https://github.com/SoulKyu/cpg/commit/a698e9f2ddeef503cf89b3ad1a39c84de841c68b))
* support ICMP, entity and CIDR rule merging in policy merge logic ([606e5f1](https://github.com/SoulKyu/cpg/commit/606e5f1a910854370ef276ab386669c16985a334))
* warn when dropped flows target reserved identities outside cpg scope ([64ef7d4](https://github.com/SoulKyu/cpg/commit/64ef7d43d1c97528e3dd48ae4c45d70627b4d201))

## [1.0.3](https://github.com/SoulKyu/cpg/compare/v1.0.2...v1.0.3) (2026-03-09)


### Bug Fixes

* remove unsupported changelog.extra field from goreleaser config ([440407c](https://github.com/SoulKyu/cpg/commit/440407c1401cf3d81c63df3cddf736ae45df040a))

## [1.0.2](https://github.com/SoulKyu/cpg/compare/v1.0.1...v1.0.2) (2026-03-09)


### Bug Fixes

* prevent duplicate rules after YAML roundtrip in policy merge ([1ba8c3a](https://github.com/SoulKyu/cpg/commit/1ba8c3a27f15f8c0b14890f2d1d2061d29ab4393))
* use unsanitized EndpointSelector to prevent label key corruption ([e064f2f](https://github.com/SoulKyu/cpg/commit/e064f2f019ecfe4a5666a6deac5a6c1a29647833))

## [1.0.1](https://github.com/SoulKyu/cpg/compare/v1.0.0...v1.0.1) (2026-03-09)


### Bug Fixes

* register OIDC auth provider for kubeconfig loading ([af20b64](https://github.com/SoulKyu/cpg/commit/af20b64a28cce81feda56917a54187f62c3e337c))

## 1.0.0 (2026-03-09)


### Features

* **01-01:** add Cilium v1.19.1 and sigs.k8s.io/yaml dependencies ([c4d1ac6](https://github.com/SoulKyu/cpg/commit/c4d1ac67f6322f7320d94f7108cc8372592b1f39))
* **01-01:** implement label selector with hierarchy and denylist ([d869ccd](https://github.com/SoulKyu/cpg/commit/d869ccdfc5c040eb846ff70bd1610621a707e00f))
* **01-01:** initialize Go module with Cobra CLI and build tooling ([ecdadf0](https://github.com/SoulKyu/cpg/commit/ecdadf08102b03464cc6d0543b3d9ee4b2ffe617))
* **01-02:** implement policy builder with flow-to-CNP transformation ([6eb3c24](https://github.com/SoulKyu/cpg/commit/6eb3c2467fcea63945dd5f8906f0a610b8439967))
* **01-02:** implement policy merge with port dedup and peer matching ([8367508](https://github.com/SoulKyu/cpg/commit/83675082143780b4e9810e2612c1256d9ea8a12d))
* **01-03:** implement output writer with merge-on-write ([d8224ce](https://github.com/SoulKyu/cpg/commit/d8224ce67cf8e815fd9a1b07265874a27dd5085b))
* **01-03:** wire CLI generate command with flags and zap logging ([cdc2694](https://github.com/SoulKyu/cpg/commit/cdc26944995bf878c916a8c02282bc8af75c262a))
* **02-01:** implement buildFilters with namespace-aware FlowFilter construction ([b88a5ff](https://github.com/SoulKyu/cpg/commit/b88a5fff3a20d2d0a01db32131ad4d30499400f6))
* **02-01:** implement StreamDroppedFlows with gRPC streaming and channel output ([4611504](https://github.com/SoulKyu/cpg/commit/46115046dd28f68e97a6a90a7f1bc6a1df8d31c8))
* **02-02:** implement aggregator with temporal flush and lost events monitor ([9ba9654](https://github.com/SoulKyu/cpg/commit/9ba965430da43e9197bca157524e911dc0f46156))
* **02-02:** implement pipeline orchestration and wire CLI generate command ([74d289f](https://github.com/SoulKyu/cpg/commit/74d289ff169627b4b7a55bcac8610f8b3b943da5))
* **03-01:** add CIDR rules for world identity and semantic policy dedup ([53172cc](https://github.com/SoulKyu/cpg/commit/53172cc4da1f8f84f99ae716691d8747f7c2769f))
* **03-01:** add file-based dedup in writer, fix merge label normalization ([056dbf1](https://github.com/SoulKyu/cpg/commit/056dbf112e7d2516e918ca9de5dc01c30f31fb54))
* **03-02:** add cross-flush dedup, cluster dedup, and auto port-forward CLI wiring ([2b6b542](https://github.com/SoulKyu/cpg/commit/2b6b542bc7414a50484873ff1b321a4cf7d76375))
* **03-02:** implement k8s package (port-forward, kubeconfig, cluster dedup) ([51efea4](https://github.com/SoulKyu/cpg/commit/51efea4bb5f6762b9fcc43fa6df8d8791f1b6803))


### Bug Fixes

* **01:** restructure plan 02 from &lt;feature&gt; to &lt;task&gt; elements ([16300fb](https://github.com/SoulKyu/cpg/commit/16300fba67b38720770312e93df63f69daa63ba2))
* **03:** cluster dedup key mismatch — use cpg- prefix for policy lookup ([4816fab](https://github.com/SoulKyu/cpg/commit/4816fab0b5f81d8dd3abc7dc424b5734bfa54b4c))
* **03:** revise plans based on checker feedback ([d43b597](https://github.com/SoulKyu/cpg/commit/d43b597d52d4ad41ea5aba6b681eb32ae98078da))
