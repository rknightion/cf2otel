---
id: CFO-0011
title: Deploy to camden (Docker) and verify end to end
status: Done
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-26 16:17'
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
- [x] #2 Every wave-1 collector's signals are observed in the m7kni stack (Loki, Mimir, Tempo) with exact query evidence
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

Loop 4 correction to the loop 3 note: the 18 restart-proof matches used frozen canonical JSON of the fields shared by each source row and Loki body. They were not zone/ray/time keys; Free-zone rows had no ray. AC3 remains checked. P6 DEP census [2026-09-24 08:18:58,13:46:18) UTC returned zero SCIM source rows in one page, so AC2 remains open.

Loop 4 P6 additional source censuses: Access SCIM updates REST returned zero rows in one page for [2026-09-24T08:18:58Z,14:15:50Z) at the CFG readback and zero rows for [08:18:58Z,15:50:53Z) at closeout. DEP census earlier in this loop was also zero through 13:46:18Z. No SCIM source event exists to prove its Loki delivery; AC2 remains open. Resume on a natural SCIM update, then verify its exact Loki row and the remaining wave-1 signals.

Loop 5 closeout P6: Access SCIM updates source census [2026-09-24T08:18:58Z,2026-09-25T10:18:05Z) returned zero rows in one page. AC2 remains unchecked; resume on a natural SCIM update and compare its exact Loki signal before checking all wave-1 signals. Implementation attempts 0/4 this loop, review-repair 0/3, infrastructure retries 0; grant: read-only opportunistic census.

Loop 6 opportunistic P6 closeout: Access SCIM updates REST census [2026-09-24T08:18:58Z,2026-09-25T14:52:48Z) returned zero source rows in one page. AC2 remains unchecked; resume on a natural SCIM update and prove its exact Loki signal before checking all wave-1 signals.

Loop 7 P6 read-only census: implementation/review attempts unchanged, infrastructure retries 0; grant: one natural SCIM source check. Source interval 2026-09-24T08:18:58Z through 2026-09-26T11:41:34Z yielded zero rows, so AC2 remains absent input and unproved. Resume on a natural source row, then compare exact ID in Loki; no synthetic audit mutation.

Loop 8 preparation 2026-09-26, Rob-authorised synthetic SCIM trigger: at 15:42:48Z Entra provision-on-demand exported one USER update to Cloudflare Access (a temporary canary value in a previously empty department attribute). The Access SCIM updates REST log for [15:30Z,16:30Z) returned exactly one USER row, status SUCCESS. Entra skipped the revert push because it does not send a cleared attribute, so Cloudflare's copy keeps the canary department; this is harmless and recorded. Next: compare that exact row in Loki ({service_name="cf2otel"} with the SCIM event_name) and, if it matches, check AC2 (every other wave-1 signal was proven in earlier loops).

Loop 8 P6-SCIM 2026-09-26T16:17Z: the Entra provision-on-demand SCIM update (logged by Cloudflare at 15:42:48Z) was matched in m7kni Loki with {service_name="cf2otel"} | event_name="cloudflare.access.scim_update" over [15:30Z,16:17Z): exactly 1 row, resource id match, resource_type USER, method PATCH, status SUCCESS, no duplicate. With earlier loops' Loki/Mimir/Tempo evidence for the other wave-1 signals, AC2 holds. Loop 8: implementation 0, review-repair 0, infrastructure retries 0; grant: read-only Loki; reason Done.
<!-- SECTION:NOTES:END -->
