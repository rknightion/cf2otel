# Changelog

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
