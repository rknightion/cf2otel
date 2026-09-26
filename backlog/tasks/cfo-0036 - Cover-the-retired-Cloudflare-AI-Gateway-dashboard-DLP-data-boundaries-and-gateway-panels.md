---
id: CFO-0036
title: >-
  Cover the retired Cloudflare AI Gateway dashboard: DLP, data boundaries and
  gateway panels
status: To Do
assignee: []
created_date: '2026-09-26 09:24'
updated_date: '2026-09-26 17:40'
labels:
  - dashboard
  - ai-gateway
dependencies: []
priority: medium
ordinal: 36000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The Infinity-based 'Cloudflare AI Gateway' dashboard (uid cloudflare-ai-gateway) and its datasource cloudflare-aigw-logs were retired 2026-09-26 in favour of cf2otel. Backup: ~/repos/chat-personal/grafana/backups/coding-agent-dashboards-20260926/cloudflare-ai-gateway.json. cf2otel already collects per-request gateway logs (cached, status, cost, wholesale, provider, model, tokens, latency) and cloudflare_ai_gateway_* metrics, but two things the old dashboard showed are not captured, and several panels have no cf2otel equivalent. Per-request traces (cf.aig.request, rootService ai-gateway) are Cloudflare's own export to Tempo and need nothing here.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 cf2otel captures DLP policy outcomes per gateway request (flagged/blocked, policy id, request vs response) from the gateway logs API, as log attributes, and a flagged-request count metric
- [x] #2 cf2otel captures the gateway metadata 'data boundaries' / exception fields the old dashboard read, or documents why the API no longer exposes them
- [ ] #3 The cf2otel dashboard's AI Gateway row adds cache-hit ratio, rate-limited (429) count, cost by provider, a per-model usage table, failed requests and DLP-flagged requests panels
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 7 AC2: read-only inspection of the retired dashboard backup found Data boundaries was static provenance text and Gateway metadata exceptions was a row grouping failed requests and DLP, not separate Logs API fields. Fifty current log-detail rows had no boundary or exception keys; docs/signals.md records this distinction and the sampled limit. Landed at c229f18 with just check and CI 36240525924 (ci-success success). AC1 and AC3 remain open: all 50 sample details and three fictional-data requests had null DLP fields, and the SDK does not specify the non-null matched-row JSON shape. Resume collector mapping only with an authoritative matched Logs API schema or a sanitized real matched detail row showing action, direction and policy ID location/cardinality; then independent REV-L36 and dashboard L36D.

Loop 7 run-end park: implementation attempts 0/4, review-repair 0/3, infrastructure retries 0; grant: bounded read-only Gateway probe and six fictional-data requests after Rob enabled Flag DLP. All 50 sampled details and all six exact synthetic log details had null dlp_action/dlp_profiles; five inspected response headers had no cf-aig-dlp, with the first header uninspected. The supplied screenshot proves policy configuration only. AC2 is documented; AC1 requires an authoritative matched Logs API schema or sanitized real matched detail with action, direction, policy ID location/cardinality, then independent REV-L36; AC3 needs L30 and L36 followed by L36D/SYNC-30.

Loop 8 preparation 2026-09-26: cause of loop 7's null DLP fields found. Every entry in the gateway policy's selected predefined profiles (Financial Information; Social Security, Insurance, Tax and Identifier Numbers) was disabled, so the Flag policy could never match; it was a configuration gap, not absent input. Custom DLP profile creation returned 403 code 3314 even with the account key. Rob enabled the Financial Information entries at about 15:58Z. At 16:04Z three fictional-data requests (published test card numbers only) were sent: one matched and two did not. Observed matched log-detail shape: dlp_action is the string FLAG (docs: FLAG or BLOCK); dlp_profiles is an array of findings, each {profile: {profile_id: uuid string, entry_ids: [uuid strings]}, policy_ids: [policy name strings, e.g. the default policy name], check: REQUEST or RESPONSE}. The cf-aig-dlp response header mirrors it as {findings: [...], action}. Non-matching rows keep dlp_action and dlp_profiles null. The one match reported check RESPONSE for a card number placed in the prompt. Multiple findings, and several policy ids per finding, are possible. Private sanitized-in-tracker receipt: codex/aigw-36/dlp-match-receipt-loop8.json. AC1 is unblocked: implement against this shape; attempts 0/4 and review-repair 0/3 are unchanged.

Loop 8 preparation, 16:09Z: the gateway logs LIST response (the collector's page source) already carries dlp_action, dlp_profiles and guardrails, alongside the detail GET. Of the newest 50 real list rows (16:03-16:08Z), 24 were flagged: all dlp_action FLAG, each with exactly one finding carrying keys check/policy_ids/profile, profile keys entry_ids/profile_id, one policy id, and check REQUEST (23) or RESPONSE (1). Natural traffic will therefore supply live AC1 proof after deploy.

Loop 8 AC1: L36 lane b642c90 plus root correction a179c32 (restored the pre-existing redacted cloudflare.ai_gateway.dlp.profiles attribute, declared the counter's {request} unit) and description fix c71d427, landed in a9335be83ec6059c078d02d647ab5a90e41eb7e3 (CI 36257122932 success incl ci-success), independent REV-L36 PASS, released v0.7.0 (Release 36257702516) and deployed to camden 17:10:35Z, healthy 10 min. Live proof: natural traffic had no flagged row after the restart, so three fictional published-test-card requests were sent 17:30:19-17:30:42Z (one flagged). Window [17:30:35Z,17:35:35Z) between two stable aigateway checkpoints: source 6 REST rows, 1 with dlp_action FLAG whose findings cover both REQUEST and RESPONSE; m7kni Mimir cloudflare_ai_gateway_dlp_requests_total went from no series at checkpoint 17:30:35Z to flagged/request 1 and flagged/response 1 (gateway label present) at 17:35:35Z, exact. Loki: that row's cloudflare.ai_gateway.request event carries dlp.action flagged, dlp.direction [request,response], dlp.policy.id, dlp.profile.id and dlp.profiles (1 of 3 sampled ids: it was the only flagged row in the window). DLP attributes also appear on the AI Gateway span via the shared attribute slice (accepted; question in the loop 8 report). AC3 pending L36D.
<!-- SECTION:NOTES:END -->
