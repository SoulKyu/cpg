# Changelog

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
