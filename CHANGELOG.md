# Changelog

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
