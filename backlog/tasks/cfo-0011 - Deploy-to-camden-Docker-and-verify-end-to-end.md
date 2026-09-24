---
id: CFO-0011
title: Deploy to camden (Docker) and verify end to end
status: Parked
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-24 08:19'
labels:
  - 'wave:1'
  - deploy
dependencies: []
priority: high
ordinal: 11000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
deploy/docker-compose.yaml reference plus the live deployment: compose project under /opt/compose/cf2otel, data under /opt/cf2otel (config 0600 owned by 65532, state dir), secrets in a root-only .env. Image from GHCR via release-please. Docker only: no Helm install.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Container healthy on camden running a released ghcr.io/rknightion/cf2otel tag
- [ ] #2 Every wave-1 collector's signals are observed in the m7kni stack (Loki, Mimir, Tempo) with exact query evidence
- [x] #3 Restart test: a container restart resumes from checkpoints without gaps or duplicates
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Parked AC2/3: v0.1.3 on Camden is healthy with six checkpoints and live HTTP/AI Gateway/Mimir/Tempo signals, but Access login and SCIM source rows were absent in the live window. Earlier failed AI Gateway windows produced 153 duplicate Loki rows; no gap/no-duplicate restart proof exists. Native export remains on.

Wave 2 v0.2.3 restart proof: AI Gateway REST source 16 IDs in 18:08:34-19:08:35 UTC; Loki has each exactly once and direct Tempo trace readback finds one matching span for each. Fifty cloudflare.http.request Loki rows in that window have zero populated cloudflare.http.ray_id attributes because rayName was unavailable in the selected GraphQL source fields. The required HTTP duplicate-ray check has no inputs, so AC3 stays open. Access SCIM source rows were zero; AC2 stays open by contract.

Additional P6 source check after v0.2.3 deploy: Access SCIM updates REST query for 2026-09-23 21:40-21:45 UTC returned zero source rows. CFO-0011 AC2 remains open by the wave 2 contract; this is absent input, not proof of delivery.

Loop 3 P1h restart proof on unchanged v0.2.3: two post-checkpoint source reads more than five minutes apart each found 18 selected-host HTTP rows. The exact Loki query returned 18 rows with 18 composite zone/ray/time keys, zero missing, extra or duplicate keys. Three AI Gateway source IDs each had one Loki event and one direct Tempo span. Private proof is retained outside tracked files. AC2 remains open: Access SCIM source query through 2026-09-24 07:05 UTC returned zero rows, so every wave-1 signal cannot be observed.

Loop 3 P6 closeout source census queried Access SCIM updates from 2026-09-24 00:18:00 to 08:18:58 UTC: zero rows in one page. The v0.3.1 container is healthy, but there is no SCIM source event to verify downstream; AC2 remains open. Resume on a natural SCIM update and verify the corresponding Loki signal before checking AC2.
<!-- SECTION:NOTES:END -->
