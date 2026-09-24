# Changelog

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
