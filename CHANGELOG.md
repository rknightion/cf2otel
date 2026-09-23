# Changelog

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
