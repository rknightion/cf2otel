# Changelog

## [0.13.0](https://github.com/rknightion/cf2otel/compare/v0.12.0...v0.13.0) (2026-10-02)


### Features

* **access:** export bounded Access and Gateway seat gauges ([707c021](https://github.com/rknightion/cf2otel/commit/707c021bd23853bd16a8b933a70266004b120931))
* **collector:** exclude discovered zones and report selection gauges ([ddb4125](https://github.com/rknightion/cf2otel/commit/ddb412590db55f0059a7d4f217edc5e0f53ee6ce))
* **grafana:** add toggleable account audit annotations ([0f1f5d2](https://github.com/rknightion/cf2otel/commit/0f1f5d2c405c1ccb8793883507df2e09169c4c3b))
* **grafana:** alert on fresh certificate pack snapshots ([91bdd5b](https://github.com/rknightion/cf2otel/commit/91bdd5b22c3f954c7f94652027c883674a12d20d))
* **grafana:** display independent Access and Gateway seats ([e5a2517](https://github.com/rknightion/cf2otel/commit/e5a2517d9098a8376d870ad34067aadc2256fe9b))
* **grafana:** show opt-in HTTP dimension rates ([88b4379](https://github.com/rknightion/cf2otel/commit/88b4379e923ffe35c1830a92eca8c52ab21c923e))
* **grafana:** show recently seen WARP devices by source tuple ([e8c8028](https://github.com/rknightion/cf2otel/commit/e8c8028157b6550ca76d50eef7fe0d65c7756252))
* **httpreq:** add opt-in colo, ASN and safe error-route counters ([ea8e9d8](https://github.com/rknightion/cf2otel/commit/ea8e9d8c1743b6638f6bec442895a867bbe55177))
* **warp:** export bounded recently seen fleet snapshots ([461185c](https://github.com/rknightion/cf2otel/commit/461185cbee8b2e3302137251434e80eed6f4b49d))

## [0.12.0](https://github.com/rknightion/cf2otel/compare/v0.11.0...v0.12.0) (2026-10-01)


### Features

* **firewall:** bound opt-in rule metric dimensions ([8e5dcee](https://github.com/rknightion/cf2otel/commit/8e5dcee8d105e297ef68d29203c987985f1d5773))
* **grafana:** add collector depth panels and Logpush failure alert ([24fb360](https://github.com/rknightion/cf2otel/commit/24fb36072e649e24ba3e6794bd7e9ddfde8b9287))
* **pushscan:** scan newly reachable commits before publication ([7ce012a](https://github.com/rknightion/cf2otel/commit/7ce012a194f6119cde9a09ef10c130c35642fe9e))
* **selfobs:** classify collector scrape errors ([59ce634](https://github.com/rknightion/cf2otel/commit/59ce6347ff6ce489f8081cd6c5350288f70e94ea))


### Bug Fixes

* **certs/telemetry:** publish paired snapshots atomically ([75c255d](https://github.com/rknightion/cf2otel/commit/75c255d6ab7e2b7e3a43c580c40a8adaae9f9a2b))
* **certs:** replace certificate gauges with expiring snapshots ([c1fc373](https://github.com/rknightion/cf2otel/commit/c1fc3734a5f2194fda24456d473da2d75d85a115))
* **grafana:** align tunnel freshness with deployment polling ([abf936c](https://github.com/rknightion/cf2otel/commit/abf936cfed87a05b6be499bdf11ab5668be250c9))
* **httpreq:** select dominant latency host variant ([a126b8a](https://github.com/rknightion/cf2otel/commit/a126b8a305e60296a797af5113fadf569cb4457e))
* **pushscan:** reject embedded IPv6 and contained literal lists ([80665a9](https://github.com/rknightion/cf2otel/commit/80665a97e5df736a0f6a1e58897b71209ee775f8))
* **relnotes:** reject escaped-label link metadata ([df7da66](https://github.com/rknightion/cf2otel/commit/df7da6625818f2c5610405e0048c57961a942f04))
* **relnotes:** reject multiline-label link metadata ([4ab6503](https://github.com/rknightion/cf2otel/commit/4ab65036fe869d134aef5d182bb9d342796794bc))
* **relnotes:** require rendered release-note input ([dedb802](https://github.com/rknightion/cf2otel/commit/dedb802d6448b25f0dbaa9d7ce87326b3b7de395))
* **rum:** omit missing web-vitals quantile points ([935007f](https://github.com/rknightion/cf2otel/commit/935007f28c2a64318c28b4e85b5a875d12f4be08))
* **telemetry:** collect gauge snapshots coherently ([202ecf6](https://github.com/rknightion/cf2otel/commit/202ecf6892c6781a2e95e2f3d7ed244a14b29acc))

## [0.11.0](https://github.com/rknightion/cf2otel/compare/v0.10.1...v0.11.0) (2026-10-01)


### Features

* **collectors:** add Durable Objects D1 and Queues depth ([7e69463](https://github.com/rknightion/cf2otel/commit/7e69463460b6002282dfc9225d1facc3edd814a3))
* **collectors:** add fail-closed platform depth preserving legacy metrics ([e82fc61](https://github.com/rknightion/cf2otel/commit/e82fc617ae508027042df584b2b928a456f3e17a))
* **healthchecks:** add fail-closed opt-in event analytics ([a5954e7](https://github.com/rknightion/cf2otel/commit/a5954e78445757b89822fcd86b8d57f236f3745c))
* **healthchecks:** collect opt-in event analytics ([5489f91](https://github.com/rknightion/cf2otel/commit/5489f911d426ce350725df23ece194b55492ccd5))
* **httpreq:** add verified visits threats and transfer KPIs ([69e47a7](https://github.com/rknightion/cf2otel/commit/69e47a758295ab7921a95f890b26f3ebee61f46e))
* **httpreq:** add visits threats and monthly transfer KPIs ([731ab6a](https://github.com/rknightion/cf2otel/commit/731ab6a0150ce4115ed97a320b29f2e8abefe988))
* **logpush:** add fail-closed account and zone failure metrics ([2bc151b](https://github.com/rknightion/cf2otel/commit/2bc151b3dce64250aacda4e5eb72e2f179c3482a))
* **logpush:** collect bounded failed uploads across account and zone scopes ([f8ebf96](https://github.com/rknightion/cf2otel/commit/f8ebf964e3e289b084c162f82a74a7c613ec0e05))
* **seam:** freeze extension signals configuration and API contracts ([58617c8](https://github.com/rknightion/cf2otel/commit/58617c833aad23448db04230b6d8e48f934f6967))
* **seam:** freeze verified extension collector contracts ([9705290](https://github.com/rknightion/cf2otel/commit/9705290995e3a7ccf80f1470458b20b6f2132035))


### Bug Fixes

* **aigateway:** bound each log commit by payload so a dense burst keeps delivering ([58ad345](https://github.com/rknightion/cf2otel/commit/58ad34531919688ed88131d4c69b0f59e9fed6dd))
* **collectors:** preserve legacy series before admitting depth metrics ([6b5326c](https://github.com/rknightion/cf2otel/commit/6b5326c9d02ed7761d9061e0283d377c55fc4a5b))
* **collectors:** reject invalid selected depth counts and null datasets ([531aec3](https://github.com/rknightion/cf2otel/commit/531aec3073f5ce453cda8122a99af029144eaf3f))
* **healthchecks:** validate raw datasets and unsigned counts ([89f4991](https://github.com/rknightion/cf2otel/commit/89f4991aadddd9ff7637919e4f805c5b129e817b))
* **httpreq:** reject null datasets when visits are selected ([1832b29](https://github.com/rknightion/cf2otel/commit/1832b29f8b627411045588b06aeb065e54c0488d))
* **httpreq:** validate visits as raw unsigned integers ([6145796](https://github.com/rknightion/cf2otel/commit/614579686dbad3ffa322b935bccfbc73735c6a72))
* **logpush:** reject null failure datasets before emission ([dbc47b5](https://github.com/rknightion/cf2otel/commit/dbc47b5c373b5e5316eb758589a785287f8ed20f))

## [0.10.1](https://github.com/rknightion/cf2otel/compare/v0.10.0...v0.10.1) (2026-10-01)


### Bug Fixes

* **aigateway:** adapt source windows after delivery deadlines ([a768eb9](https://github.com/rknightion/cf2otel/commit/a768eb9efee9de1d90ff1ebbbfc47526e891875f))
* **aigateway:** subdivide source windows on aggregate commit deadlines ([836cbe7](https://github.com/rknightion/cf2otel/commit/836cbe7ade27ac49ada5c7ce9bab6d807d84c1ab))

## [0.10.0](https://github.com/rknightion/cf2otel/compare/v0.9.0...v0.10.0) (2026-10-01)


### Features

* **apidrift:** probe HTTP bytes, Workers invocations, tunnels and certificates ([000d210](https://github.com/rknightion/cf2otel/commit/000d2101980788e638927500a4c75ac4e6b1877c))
* **certs:** export certificate pack expiry snapshots ([f3fbcad](https://github.com/rknightion/cf2otel/commit/f3fbcad94b7c9ed77eae6873efb73dc583548645))
* **dashboard:** cover landed signals and current tunnel health ([5ee8e69](https://github.com/rknightion/cf2otel/commit/5ee8e692749ec1325bd6aa87cf0407a606349963))
* **httpreq:** export zone-level HTTP breakdowns with aliased batches ([6b4b7be](https://github.com/rknightion/cf2otel/commit/6b4b7becdbec7a317f83d3ccd4bd532dca781e77))
* **httpreq:** request totals now count eyeball traffic only; add bytes and latency ([a79211e](https://github.com/rknightion/cf2otel/commit/a79211e944e843d66cec09c4d722873363054b55))
* **tunnels:** export snapshot health and status changes ([efe59b6](https://github.com/rknightion/cf2otel/commit/efe59b61e83f9dad76ddb7de13e89cc9d9b1787e))
* **workers:** export aggregate invocation counters and timing quantiles ([8396656](https://github.com/rknightion/cf2otel/commit/83966563875d78bde91ea012e20497c0783b9c7b))


### Bug Fixes

* **aigateway:** align coverage windows with the scheduler clock ([3166014](https://github.com/rknightion/cf2otel/commit/316601492bca4d2c826a07beab62819f46bc4e22))
* **aigateway:** normalize model identifiers and malformed values ([c1facc9](https://github.com/rknightion/cf2otel/commit/c1facc9e897bcfee95092587bcc894d8ee702340))
* **certs:** reject null results and count logical permission errors ([e586bdd](https://github.com/rknightion/cf2otel/commit/e586bdd5f9b9cb56cfa7586cc2de5acd17df3e28))
* **dashboard:** show non-JSON AI bodies and correct RUM source units ([99f662b](https://github.com/rknightion/cf2otel/commit/99f662b407666949222813b1c2a9cfbb0aafa742))
* **dns:** bound lifetime series with coarse aggregation ([3321227](https://github.com/rknightion/cf2otel/commit/3321227255fb9e5f2710368ffac8c13a99358b45))
* **dns:** configure SDK cardinality and expose metric overflows ([6c7d6c6](https://github.com/rknightion/cf2otel/commit/6c7d6c65a5e1e6b0d476c17fdfc2f8730d2e9f58))
* **selfobs:** record identity outcomes as request counter deltas ([8e67b69](https://github.com/rknightion/cf2otel/commit/8e67b69034557c2b8db11fdeb98996fbed63d401))
* **selfobs:** refine poll duration histogram buckets ([2384dff](https://github.com/rknightion/cf2otel/commit/2384dffb17625fabc46916a53477c37ffe2a0d34))
* **tunnels:** retire stale gauges and freeze transition retries ([b82aad9](https://github.com/rknightion/cf2otel/commit/b82aad9861840f795d0e44795f252f68800c368e))

## [0.9.0](https://github.com/rknightion/cf2otel/compare/v0.8.2...v0.9.0) (2026-09-30)


### Features

* **dashboard:** rebuild the Grafana dashboard with an overview and per-domain tabs ([bd6eb99](https://github.com/rknightion/cf2otel/commit/bd6eb9939ec292830616d6484c1d7dba16d1553d))
* **seam:** declare frozen analytics signals and configuration ([4a37f5f](https://github.com/rknightion/cf2otel/commit/4a37f5ffb06256c55324c6a67aff303c1ad08a46))


### Bug Fixes

* **access:** normalize allowed decisions and country codes ([1b5bc66](https://github.com/rknightion/cf2otel/commit/1b5bc66e581077577e8da46454526c5f5a3e8407))
* **aigateway:** keep metadata export for non-JSON bodies ([14ef1db](https://github.com/rknightion/cf2otel/commit/14ef1dbbaccbc56a7f7a74b6297b3cd67a78af74))
* **deps:** update module go.opentelemetry.io/proto/otlp to v1.11.1 ([#30](https://github.com/rknightion/cf2otel/issues/30)) ([d5e48bd](https://github.com/rknightion/cf2otel/commit/d5e48bdc3d17950b973479db0abebab8b461369a))
* **rum:** reduce Web Vitals timing scale by 1000x to seconds ([f68e1dd](https://github.com/rknightion/cf2otel/commit/f68e1dd8851103eb9933ffc74a2178934f99f04e))

## [0.8.2](https://github.com/rknightion/cf2otel/compare/v0.8.1...v0.8.2) (2026-09-29)


### Bug Fixes

* **apidrift:** treat scoped null fields as missing ([0aba544](https://github.com/rknightion/cf2otel/commit/0aba54402c31a9c0fabb50ca4339a57169229e66))

## [0.8.1](https://github.com/rknightion/cf2otel/compare/v0.8.0...v0.8.1) (2026-09-26)


### Bug Fixes

* **config:** allow environment overrides for dotted collector names ([f59ebbf](https://github.com/rknightion/cf2otel/commit/f59ebbf16fb50b2b496957b455e18988bc7ed243))
* **config:** allow environment overrides for dotted collector names ([d98311c](https://github.com/rknightion/cf2otel/commit/d98311cca2ffe44d78a898e5aa872ab1ce0e54b7))

## [0.8.0](https://github.com/rknightion/cf2otel/compare/v0.7.0...v0.8.0) (2026-09-26)


### Features

* **aigateway:** report REST log coverage against GraphQL request counts ([dacf99b](https://github.com/rknightion/cf2otel/commit/dacf99bd89a560174b7a264c8831e69530793e67))
* **aigateway:** report REST log coverage against GraphQL request counts ([de07aa7](https://github.com/rknightion/cf2otel/commit/de07aa7688e71c2cc28e539bb3484d33b8342e1e))
* **grafana:** add AI Gateway parity panels ([6019c01](https://github.com/rknightion/cf2otel/commit/6019c019c99da5dd32cc25c9a35ce75fe1558fb5))
* **grafana:** add AI Gateway parity panels ([3fbb557](https://github.com/rknightion/cf2otel/commit/3fbb5575f1e2fe1c9ee11dd0e8ae2c943ce4642e))
* **grafana:** cover post-wave-1 collectors and data-loss alerts ([a253d0b](https://github.com/rknightion/cf2otel/commit/a253d0b4fc463218af8a6c764ab3aa6edc3874d7))
* **grafana:** cover post-wave-1 collectors and data-loss alerts ([234b425](https://github.com/rknightion/cf2otel/commit/234b4250eb88387994eecb3fab58495d3f06f8c3))


### Bug Fixes

* **aigateway:** pin coverage cadence and end each tick on a window boundary ([b01ed38](https://github.com/rknightion/cf2otel/commit/b01ed38b6ba5fdc88aabde70dae41bc92d32c990))
* **aigateway:** reject a coverage range spanning more than one window ([43b14ee](https://github.com/rknightion/cf2otel/commit/43b14eea9fd9e396afee437caf4d3b4b743a1a99))
* **aigateway:** skip null DLP findings ([b4b755e](https://github.com/rknightion/cf2otel/commit/b4b755e783298fb327caed300cafbbca293bf2bd))
* **aigateway:** skip null DLP findings and refuse multi-window coverage ranges ([dfc5de6](https://github.com/rknightion/cf2otel/commit/dfc5de603ee5b3b50937ec319aeda43b0e8778bf))

## [0.7.0](https://github.com/rknightion/cf2otel/compare/v0.6.0...v0.7.0) (2026-09-26)


### Features

* **aigateway:** capture DLP policy outcomes ([a9335be](https://github.com/rknightion/cf2otel/commit/a9335be83ec6059c078d02d647ab5a90e41eb7e3))
* **aigateway:** capture DLP policy outcomes ([b642c90](https://github.com/rknightion/cf2otel/commit/b642c901371fff795b03289f4ec759db92427398))


### Bug Fixes

* **aigateway:** keep the DLP profiles attribute and declare the DLP metric unit ([a179c32](https://github.com/rknightion/cf2otel/commit/a179c3217d346a796a9390da290d7ab15829cad8))


### Refactoring

* **cfapi:** typed GraphQL saturation error and shared window splitter ([ae1ad8d](https://github.com/rknightion/cf2otel/commit/ae1ad8da857fd85980b515ec31a584189187ec4d))
* **cfapi:** typed GraphQL saturation error and shared window splitter ([f45209a](https://github.com/rknightion/cf2otel/commit/f45209ae13adf9c7bc9084bb0f450db0384fcf8a))

## [0.6.0](https://github.com/rknightion/cf2otel/compare/v0.5.1...v0.6.0) (2026-09-26)


### ⚠ BREAKING CHANGES

* **telemetry:** Prometheus series gain seconds or ratio suffixes where the declared units require them; see docs/signals.md for the old-to-new names.

### Features

* add release notes coverage check ([7839573](https://github.com/rknightion/cf2otel/commit/7839573299b9c010c888a9c66c15ffbe53961080))
* add release notes coverage check ([9ad1dcd](https://github.com/rknightion/cf2otel/commit/9ad1dcd734e5f393a359cd11306f9d6c4035dd9c))
* **apidrift:** cover Cloudflare dataset and AI Gateway contracts ([1daf4be](https://github.com/rknightion/cf2otel/commit/1daf4be0988d4cb14cb68c374a183beb664bf2ce))
* collect Email Routing and Sending Groups metrics ([8f2dacf](https://github.com/rknightion/cf2otel/commit/8f2dacf462c712fd67e52dd006abecfd8303dcb7))
* **telemetry:** declare metric units, descriptions and explicit histogram buckets ([ac8958c](https://github.com/rknightion/cf2otel/commit/ac8958cb7b4f9c69ac91f73c78c5faca88b7287f))


### Bug Fixes

* **apidrift:** handle unavailable datasets and log endpoints ([9d17caf](https://github.com/rknightion/cf2otel/commit/9d17caf36ca70454346d652f16163d9577337743))
* classify email retention gaps ([e19204b](https://github.com/rknightion/cf2otel/commit/e19204bea58c7772e60d11ad6ee54f7bbfa938c6))
* **email:** fail closed on query retention gaps ([9bb6ff8](https://github.com/rknightion/cf2otel/commit/9bb6ff8d5adb73fd852b159cb35a1121516cc7e7))
* **email:** fix email retention gap handling ([81ff240](https://github.com/rknightion/cf2otel/commit/81ff240db81959097138694eea04a721880b4e1b))
* **email:** respect email dataset page size ([e55e851](https://github.com/rknightion/cf2otel/commit/e55e851177094965db945297df298cad9defb606))
* fail closed on email zone selection gaps ([cd52578](https://github.com/rknightion/cf2otel/commit/cd52578081e302c6cf1caaeb7e45531becf26e92))
* preserve checkpoint state in dry runs ([13353e2](https://github.com/rknightion/cf2otel/commit/13353e2904833c50614ff9b5f10bb6be3ccc654f))
* **relnotes:** treat empty restore commits as subject aliases ([198d4dc](https://github.com/rknightion/cf2otel/commit/198d4dc464ef1027bb2a188e0d935dc94b42f6f9))
* simplify email duration limit check ([5a93044](https://github.com/rknightion/cf2otel/commit/5a93044f88375bfd8e2294659fc31500ec2259a8))
* **telemetry:** preserve old and new series during unit rollout ([faf46e7](https://github.com/rknightion/cf2otel/commit/faf46e706de9afabf7e667c7995667815ef4e880))

## [0.5.1](https://github.com/rknightion/cf2otel/compare/v0.5.0...v0.5.1) (2026-09-25)


### Bug Fixes

* normalize default max field selections ([fe0a438](https://github.com/rknightion/cf2otel/commit/fe0a438ec9fd270d67cf364f0a9f9ec830e5628c))
* select flattened max fields in GraphQL ([977fa1c](https://github.com/rknightion/cf2otel/commit/977fa1cde34e460662357981b10c241cee8c6af0))

## [0.5.0](https://github.com/rknightion/cf2otel/compare/v0.4.0...v0.5.0) (2026-09-25)


### Features

* add platform analytics dashboard panels ([182b285](https://github.com/rknightion/cf2otel/commit/182b285d6b24e2d683ac837c5ac324d312a4dcc1))
* collect Cloudflare Queue metrics ([cb52517](https://github.com/rknightion/cf2otel/commit/cb525175cfe590032b7c57b4250d6d4a01ebd21f))
* collect D1 and KV account metrics ([0306489](https://github.com/rknightion/cf2otel/commit/03064897e82bf8d7f508af382045987075198367))
* collect Durable Objects analytics ([c7f9f88](https://github.com/rknightion/cf2otel/commit/c7f9f88ceaaf9fcb3d4441f8fc222e348320db4d))
* collect R2 analytics ([461b811](https://github.com/rknightion/cf2otel/commit/461b811616e304313075588ae75b058fbfbc083d))
* collect Workers Turnstile and Logpush analytics ([0e0c6d0](https://github.com/rknightion/cf2otel/commit/0e0c6d031c8125d7c2e93e8c263b1882217751c4))
* freeze platform collector metric seams ([dad5e8e](https://github.com/rknightion/cf2otel/commit/dad5e8e98e8a8b1c85306bb4ef5366924cf9024b))


### Bug Fixes

* **deps:** update module github.com/knadh/koanf/v2 to v2.3.7 ([#17](https://github.com/rknightion/cf2otel/issues/17)) ([69308ec](https://github.com/rknightion/cf2otel/commit/69308ec6b43700a30649e0ba04efda8b705f0e47))
* fail R2 windows without a complete bucket ([465b38e](https://github.com/rknightion/cf2otel/commit/465b38ec353d13b1e2d2436ef683802cf4cfadad))
* hold checkpoints until a complete platform bucket ([d87577e](https://github.com/rknightion/cf2otel/commit/d87577ed933e0e17567ad1793bcaae3d9059b42b))
* hold Queue checkpoint until a complete bucket ([26e9a28](https://github.com/rknightion/cf2otel/commit/26e9a286531c8a48b2697300690b0d3af9e42241))
* name queue snapshot gauges by aggregation ([c2a386c](https://github.com/rknightion/cf2otel/commit/c2a386c7090ac5eb5b5a70b47683b89c5a620f29))
* split platform Groups on bucket boundaries ([66ac178](https://github.com/rknightion/cf2otel/commit/66ac178afa8f02d2307f11b61c9ce2791f351089))
* use lowercase Durable Objects errors ([83079be](https://github.com/rknightion/cf2otel/commit/83079bec4f714a4310019bad7b04023c1d0e1b70))
* use lowercase Queue error strings ([8282104](https://github.com/rknightion/cf2otel/commit/8282104678bf4a5500031c541aa05dd8a403c0bd))

## [0.4.0](https://github.com/rknightion/cf2otel/compare/v0.3.1...v0.4.0) (2026-09-24)


### Features

* add bounded HTTP metrics scope controls ([76372a8](https://github.com/rknightion/cf2otel/commit/76372a83294c4c62b0e38fb5908679ec813a9707))
* collect Gateway DNS group metrics ([57749f7](https://github.com/rknightion/cf2otel/commit/57749f7d232be1e3d092220bc4edd7235ff63862))
* support bounded HTTP metrics across account zones ([bf19cd2](https://github.com/rknightion/cf2otel/commit/bf19cd212bedc8bce8292649d1a9ef8e08a89005))


### Bug Fixes

* classify Gateway DNS resolver decisions ([27c4f6e](https://github.com/rknightion/cf2otel/commit/27c4f6efa1c960da744514e998133996ca99450f))
* constrain explicit HTTP metric zones to account ([20e9981](https://github.com/rknightion/cf2otel/commit/20e9981f0c793b000e37109a15b896f880e1d69d))
* negotiate Gateway DNS metric dimensions ([e0cca2f](https://github.com/rknightion/cf2otel/commit/e0cca2f8443e0d54f47fa518a29b63e0423ef698))
* omit HTTP no-origin duration sentinel ([7c07e16](https://github.com/rknightion/cf2otel/commit/7c07e1620c3b65d6cd03b90f5773fd0743e125c4))
* restrict all-zone HTTP metrics to configured account ([18205c9](https://github.com/rknightion/cf2otel/commit/18205c9e1a92bab808808811b1568998d0304ab0))
* retain Gateway DNS sums for empty dimensions ([0ca7c56](https://github.com/rknightion/cf2otel/commit/0ca7c56b95d66e0691031213e0af116ff6b81300))

## [0.3.1](https://github.com/rknightion/cf2otel/compare/v0.3.0...v0.3.1) (2026-09-24)


### Bug Fixes

* select RUM quantiles from advertised GraphQL fields ([f7c9e3a](https://github.com/rknightion/cf2otel/commit/f7c9e3a7a9f6c6e0017deb65db6c6130d736c1b2))

## [0.3.0](https://github.com/rknightion/cf2otel/compare/v0.2.3...v0.3.0) (2026-09-24)


### Features

* catch up bounded collector windows ([1780b2a](https://github.com/rknightion/cf2otel/commit/1780b2a03247174805194f3081cf29611b359dc3))
* collect DNS analytics events and counts ([8c57018](https://github.com/rknightion/cf2otel/commit/8c57018e3f63471c92482c7cc87ee35ab5264247))
* collect RUM pageload and web vitals groups ([7e783dc](https://github.com/rknightion/cf2otel/commit/7e783dc88eb81024f61ca8f5d5a4e25517d87a43))
* declare catch-up window metric ([3ad6f42](https://github.com/rknightion/cf2otel/commit/3ad6f42f9dae9a0b2fed56486539474dc12c19f2))
* declare DNS collector seams ([7c7f8af](https://github.com/rknightion/cf2otel/commit/7c7f8afd5dcc333a38bbc86c6ae68934fdf235aa))
* declare RUM collector seams ([f610b0a](https://github.com/rknightion/cf2otel/commit/f610b0abc4fb5d955d3969e1df007ea41c3d0cda))
* enable RUM collectors and document signals ([e80a195](https://github.com/rknightion/cf2otel/commit/e80a1951f67afda912a0ac02fe5e05fd1aca8f1a))


### Bug Fixes

* bound Access replay fingerprints ([4aa73a2](https://github.com/rknightion/cf2otel/commit/4aa73a230e8df209a0cabe8c555808569ee1e5ec))
* deduplicate replayed Access identity rows ([af05fe9](https://github.com/rknightion/cf2otel/commit/af05fe94277b94377eed711f0213774031cb741e))
* hold audit windows for measured source lag ([64a47dc](https://github.com/rknightion/cf2otel/commit/64a47dc6d5ab33bf94e00404edaf2e9d4e636457))
* identify catch-up metric as non-secret ([8db8e9e](https://github.com/rknightion/cf2otel/commit/8db8e9e1d74b1a3304bd63c89f239323e97dc048))
* keep DNS retry errors scoped to zone ([de31f9c](https://github.com/rknightion/cf2otel/commit/de31f9ca99652d5425f12e7be8658745e3fc5506))
* keep DNS seam disabled until collector lands ([45e4b3b](https://github.com/rknightion/cf2otel/commit/45e4b3bcd38143bc6316bbcbabf0a838e268021c))
* preserve DNS zones across retention gaps ([e2dc9b3](https://github.com/rknightion/cf2otel/commit/e2dc9b3ed5c079169a72d5087b3fd59ba4e6519e))
* reject empty DNS zone discovery ([1420f2c](https://github.com/rknightion/cf2otel/commit/1420f2ce876adc978eeedab4774c97a2e75ed289))
* repeat stale RUM gauges after export retry ([bbb14c6](https://github.com/rknightion/cf2otel/commit/bbb14c60f8e995a3426dc49867d5776ee0eac98e))
* skip disabled discovered DNS zones ([8359b12](https://github.com/rknightion/cf2otel/commit/8359b125e1d580c4ed3a667a52d3f13aa3386202))
* stabilize Access replay under capacity overflow ([84e72a7](https://github.com/rknightion/cf2otel/commit/84e72a7c90db168bc08b77a8a1d9c42f8cc0ee20))
* use lowercase collector errors ([b7542ae](https://github.com/rknightion/cf2otel/commit/b7542ae95b76b407f46e667a964bc656bf1a461b))

## [0.2.3](https://github.com/rknightion/cf2otel/compare/v0.2.2...v0.2.3) (2026-09-23)


### Bug Fixes

* fail closed on missing configured firewall zones ([8e66817](https://github.com/rknightion/cf2otel/commit/8e66817bde28c341d244cfe6e7d8ca3d51ed4f9f))

## [0.2.2](https://github.com/rknightion/cf2otel/compare/v0.2.1...v0.2.2) (2026-09-23)


### Bug Fixes

* bound AI Gateway catch-up export windows ([a3fa01b](https://github.com/rknightion/cf2otel/commit/a3fa01b2a16c2f8fd56381848437406f44330774))

## [0.2.1](https://github.com/rknightion/cf2otel/compare/v0.2.0...v0.2.1) (2026-09-23)


### Bug Fixes

* handle unavailable AI Gateway bodies and firewall schema ([1b7ca51](https://github.com/rknightion/cf2otel/commit/1b7ca51ccfec438729719e9f1d0fd6667ece37c2))

## [0.2.0](https://github.com/rknightion/cf2otel/compare/v0.1.3...v0.2.0) (2026-09-23)


### Features

* deliver atomic windows and expand Cloudflare signals ([c1ce41d](https://github.com/rknightion/cf2otel/commit/c1ce41d85756bd5a80b8b871d706c74324b9be4f))
* freeze wave 2 collector and telemetry seams ([732ea5a](https://github.com/rknightion/cf2otel/commit/732ea5a62406118b1d5311748d298828c429cb8c))


### Bug Fixes

* batch logs with backpressure and bound exporter retries ([0527521](https://github.com/rknightion/cf2otel/commit/052752158952c5e0d3a51fc9892f5c358b38dc5d))
* defer stub scheduling and validate span logs ([215c357](https://github.com/rknightion/cf2otel/commit/215c357f3b138c98592fd7033ccebcbb68803e17))
* make OTLP delivery lossless and redact exporter errors ([ba9874b](https://github.com/rknightion/cf2otel/commit/ba9874b1afe48ebea214a2b1e131b1f8af487efd))
* preserve first cursor and bound retry windows ([1cda73d](https://github.com/rknightion/cf2otel/commit/1cda73d144d23cda5ec18a9c7ceb032584e1ca7b))
* use current attribute string API for batch estimates ([ef4a657](https://github.com/rknightion/cf2otel/commit/ef4a657d7bdfd5dcdc5af6264e04ece416bb6c80))

## [0.1.3](https://github.com/rknightion/cf2otel/compare/v0.1.2...v0.1.3) (2026-09-23)


### Bug Fixes

* retain AI Gateway spans when response body is absent ([10a1758](https://github.com/rknightion/cf2otel/commit/10a1758aca6858d2a7ef8c0afa943868e9c81724))

## [0.1.2](https://github.com/rknightion/cf2otel/compare/v0.1.1...v0.1.2) (2026-09-23)


### Bug Fixes

* read AI Gateway bodies as raw JSON ([802cbc3](https://github.com/rknightion/cf2otel/commit/802cbc3c4c2d8cd635e5d7fad3938ee5fa757cc1))

## [0.1.1](https://github.com/rknightion/cf2otel/compare/v0.1.0...v0.1.1) (2026-09-23)


### Bug Fixes

* cap AI Gateway log pages at live API limit ([24fb0cd](https://github.com/rknightion/cf2otel/commit/24fb0cdabb5ab59ddc3ece371904479945c18680))

## 0.1.0 (2026-09-23)


### Features

* complete identity observability and Grafana alerts ([508c929](https://github.com/rknightion/cf2otel/commit/508c92973ef33812452d7f370b32ead61efc2ce6))
* freeze wave-1 exporter seams ([1aa4747](https://github.com/rknightion/cf2otel/commit/1aa47470ad8682a99cedf93ce7a47b2f1dba11de))
* ship first Cloudflare exporter collectors and delivery assets ([8a7effc](https://github.com/rknightion/cf2otel/commit/8a7effc883622254cb027320a382ce64321311c3))


### Bug Fixes

* accept singleton Grafana resource readback ([696e892](https://github.com/rknightion/cf2otel/commit/696e8928e8d697e7d2105f039e96fbbdae2ca1b1))
* restore API drift and Grafana sync gates ([4268e62](https://github.com/rknightion/cf2otel/commit/4268e627e74a95ca927e86308fb7d0da8c674a12))
