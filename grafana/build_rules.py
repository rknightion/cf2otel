#!/usr/bin/env python3
"""Generate cf2otel Grafana-managed alert rules."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path

from build_dashboard import (ACCESS_SEATS_INTERVAL_SECONDS, CERT_FRESH, CERTS_PACKS_FRESHNESS_SECONDS,
                             CERTS_PACKS_INTERVAL_SECONDS, WARP_INTERVAL_SECONDS, render, warp_deployment_seconds)

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "alerts" / "grafana-managed"
FOLDER = "REPLACE_WITH_FOLDER_UID"
PROM = "grafanacloud-prom"


def tunnels_status_interval_seconds() -> int:
    """Read the deployment-aligned generator setting, not a runtime config key.

    The exporter's built-in tunnels.status default is 60s, but the deployment
    polls every 300s. A 60s default gave a 180s freshness gate that expired
    between polls and flapped NoData, so the generator default follows the
    deployment. Regenerate with this variable for any other interval.
    """
    key = "GRAFANA_TUNNELS_STATUS_INTERVAL_SECONDS"
    value = os.environ.get(key, "300")
    if not value.isascii() or not value.isdecimal():
        raise SystemExit(f"{key} must be a positive integer in seconds")
    try:
        interval = int(value)
    except ValueError:
        raise SystemExit(f"{key} must be a positive integer in seconds") from None
    if interval <= 0:
        raise SystemExit(f"{key} must be a positive integer in seconds")
    return interval


TUNNELS_STATUS_INTERVAL_SECONDS = tunnels_status_interval_seconds()
TUNNELS_STATUS_FRESHNESS_SECONDS = 3 * TUNNELS_STATUS_INTERVAL_SECONDS

# A newly observed positive counter has no zero baseline: increase() alone misses
# its first failure. Include first-seen series until older history exists. Search
# the prior 15-minute window, not one offset instant, so a sampling gap does not
# turn an unchanged existing counter into a new failure. This also covers restarts.
LOGPUSH_FINAL = 'cloudflare_logpush_failed_uploads_total{service_name="cf2otel",cloudflare_logpush_final_attempt="true",cloudflare_logpush_status_code=~"[3-9][0-9][0-9]"}'
LOGPUSH_FINAL_FAILURE = f'(increase({LOGPUSH_FINAL}[15m]) > 0) or ({LOGPUSH_FINAL} unless max_over_time({LOGPUSH_FINAL}[15m] offset 15m))'

# Snapshot gauges retire removed packs and old status labels atomically. Match
# presence before aggregating expiry, so an unknown expiry never becomes zero.
CERT_PACK = 'cloudflare_certificate_pack{service_name="cf2otel"}'
CERT_ID = 'cloudflare_certificate_zone, cloudflare_certificate_pack_id'
CERT_EXPIRY = f'max by ({CERT_ID}) (((cloudflare_certificate_expiry_seconds{{service_name="cf2otel"}} < bool 1209600) and ({CERT_PACK} == 1)) and on(instance) {CERT_FRESH})'
CERT_STATUS = f'(max by ({CERT_ID}) ((cloudflare_certificate_pack{{service_name="cf2otel",cloudflare_certificate_status!~"active|backup_issued"}} == 1) and on(instance) {CERT_FRESH}) or on ({CERT_ID}) (0 * max by ({CERT_ID}) (({CERT_PACK} == 1) and on(instance) {CERT_FRESH})))'
CERT_DESCRIPTION = (f"Requires opt-in certs.packs and collector last success less than {CERTS_PACKS_FRESHNESS_SECONDS} seconds old "
                    f"(three deployment polling intervals of {CERTS_PACKS_INTERVAL_SECONDS} seconds). Removed packs and previous statuses retire. "
                    "Absent/stale data is not proof of health. For a non-default interval regenerate with GRAFANA_CERTS_PACKS_INTERVAL_SECONDS matching the deployment.")

# Collector staleness is three polling intervals per collector. Intervals mirror
# config.Default().Collectors[...].Interval in internal/config/config.go; every
# collector not listed polls every DEFAULT_COLLECTOR_INTERVAL_SECONDS
# (GRAFANA_DEFAULT_COLLECTOR_INTERVAL_SECONDS overrides it). Each
# entry can be overridden at generation time with GRAFANA_<NAME>_INTERVAL_SECONDS
# to match the deployment. This is generator-side only, not an exporter key.
DEFAULT_COLLECTOR_INTERVAL_SECONDS = warp_deployment_seconds("GRAFANA_DEFAULT_COLLECTOR_INTERVAL_SECONDS", 300)
STALE_PERIODS = 3
COLLECTOR_INTERVAL_SECONDS = {
    "access.seats": ACCESS_SEATS_INTERVAL_SECONDS,
    "certs.packs": CERTS_PACKS_INTERVAL_SECONDS,
    "httpreq.threats": warp_deployment_seconds("GRAFANA_HTTPREQ_THREATS_INTERVAL_SECONDS", 3600),
    "httpreq.transfer": warp_deployment_seconds("GRAFANA_HTTPREQ_TRANSFER_INTERVAL_SECONDS", 3600),
    "tunnels.status": TUNNELS_STATUS_INTERVAL_SECONDS,
    "warp.fleet": WARP_INTERVAL_SECONDS,
}


def stale_classes() -> list[tuple[int, str]]:
    """(threshold seconds, collector selector) per interval class.

    Collectors whose interval equals the default join the catch-all class, which
    matches every collector not named elsewhere, including unknown ones.
    """
    by_threshold: dict[int, list[str]] = {}
    for name, interval in sorted(COLLECTOR_INTERVAL_SECONDS.items()):
        if interval != DEFAULT_COLLECTOR_INTERVAL_SECONDS:
            by_threshold.setdefault(STALE_PERIODS * interval, []).append(name)
    named = [n for names in by_threshold.values() for n in names]
    classes = [(STALE_PERIODS * DEFAULT_COLLECTOR_INTERVAL_SECONDS,
                f'cf2otel_collector!~"{"|".join(sorted(named))}"' if named else 'cf2otel_collector!=""')]
    classes += [(threshold, f'cf2otel_collector=~"{"|".join(names)}"') for threshold, names in sorted(by_threshold.items())]
    return classes


def collector_stale_expr() -> str:
    """Seconds beyond each collector's own threshold; fires when greater than 0.

    One selector per interval class, ORed. Classes are disjoint, so no series
    appears twice. A value of 0 or below means fresh; the rule keeps NoData as
    Alerting, so the query must keep returning fresh collectors rather than
    filtering them out.
    """
    return " or ".join(
        f'((time() - max by (cf2otel_collector) (cf2otel_scrape_last_success_timestamp_seconds{{service_name="cf2otel",{selector}}})) - {threshold})'
        for threshold, selector in stale_classes())


# Backlog score above which a collector is catching up rather than slow.
BACKLOG_INTERVALS = 3


def poll_health_expr(metric: str) -> str:
    """Normalize within each replica before reducing to one value per collector."""
    return (f'max by (cf2otel_collector) (cf2otel_{metric}_seconds{{service_name="cf2otel"}} '
            '/ (cf2otel_scrape_interval_seconds{service_name="cf2otel"} > 0))')


def collector_stale_description() -> str:
    classes = stale_classes()
    named = [f"{threshold} seconds for {selector.split('=~', 1)[1].strip(chr(34)).replace('|', ', ')}" for threshold, selector in classes[1:]]
    parts = ", ".join(named + [f"{classes[0][0]} seconds for every other collector"])
    return ("Collector last-success timestamp is older than three of that collector's polling intervals "
            f"({parts}). The query value is seconds beyond the threshold, so only a value above 0 fires. "
            "Regenerate with the matching GRAFANA_*_INTERVAL_SECONDS settings for non-default deployment intervals.")


RULES = [
    ("cf2otel-certificate-expiry", "Cloudflare certificate expires in under 14 days", "Earliest observed expiry in a present pack is under 14 days, including already expired packs. Seconds to expiry are observed at the last successful poll, not a live countdown. " + CERT_DESCRIPTION, 2640,
     CERT_EXPIRY, 0, "NoData"),
    ("cf2otel-certificate-status", "Cloudflare certificate pack is not active", "A present certificate pack has a status other than active or backup_issued (Cloudflare's normal standby state for a second-CA backup certificate), including packs with unknown expiry. " + CERT_DESCRIPTION, 2645,
     CERT_STATUS, 0, "NoData"),
    ("cf2otel-tunnel-unhealthy", "Cloudflare tunnel is not healthy", f"The current (value 1) tunnel status has been non-healthy for five minutes, with collector last success less than {TUNNELS_STATUS_FRESHNESS_SECONDS} seconds old (three deployment polling intervals of {TUNNELS_STATUS_INTERVAL_SECONDS} seconds). Retired value-0 states are excluded. The deployment polls tunnels.status every 300 seconds (the exporter's built-in default is 60), so the generator default is 300; regenerate with GRAFANA_TUNNELS_STATUS_INTERVAL_SECONDS for any other interval. Collector is disabled by default; stale/absent data is not proof of health.", 2641,
     '(max by (cloudflare_tunnel_id, cloudflare_tunnel_name) (cloudflare_tunnel_status{service_name="cf2otel",cloudflare_tunnel_status!="healthy"} == 1) or on (cloudflare_tunnel_id, cloudflare_tunnel_name) (0 * max by (cloudflare_tunnel_id, cloudflare_tunnel_name) (cloudflare_tunnel_status{service_name="cf2otel"} == 1))) and on() (time() - max(cf2otel_scrape_last_success_timestamp_seconds{service_name="cf2otel",cf2otel_collector="tunnels.status"}) < ' + str(TUNNELS_STATUS_FRESHNESS_SECONDS) + ')', 0, "NoData"),
    ("cf2otel-collector-stale", "cf2otel collector is stale", collector_stale_description(), 401,
     collector_stale_expr(), 0, "Alerting"),
    ("cf2otel-collector-slow", "cf2otel collector polling exceeds its interval", "An active poll's elapsed time or the last completed poll's duration exceeds the collector's configured runtime interval for five minutes. Includes window and snapshot collectors; successful polls can still be slow. The query value is the worst elapsed-to-interval ratio across instances, calculated separately for each instance before reduction. Inspect collector status, API limiter waits and OTLP export latency. Silent while the same collector's checkpoint backlog rule is over its threshold, because catch-up polls run long by design and that rule already covers them.", 401,
     f'{poll_health_expr("scrape_elapsed")} unless on (cf2otel_collector) ({poll_health_expr("checkpoint_backlog")} > {BACKLOG_INTERVALS})', 1, "Alerting"),
    ("cf2otel-checkpoint-backlog", "cf2otel collector checkpoint backlog exceeds three intervals", "A window collector's unprocessed source time exceeds three configured runtime polling intervals for five minutes. Backlog excludes intentional source holdback and complete-bucket alignment, so it can grow despite successful pulls. The query value is the worst backlog-to-interval ratio across instances, calculated separately for each instance before reduction. Snapshot and disabled collectors have no backlog series. Absent backlog data is not proof of health; use the collector-stale rule for missing polling telemetry. Compare checkpoint age and catch-up progress before changing polling or body capture settings; preserve checkpoints.", 404,
     poll_health_expr("checkpoint_backlog"), BACKLOG_INTERVALS, "NoData"),
    ("cf2otel-export-failure", "cf2otel export is failing", "OTLP export failures occurred in the last 15 minutes.", 403,
     'sum(increase(cf2otel_export_errors_total{service_name="cf2otel"}[15m])) or on() (0 * sum(increase(cf2otel_export_success_total{service_name="cf2otel"}[15m])))', 0, "Alerting"),
    ("cf2otel-window-gap", "cf2otel window gap detected", "A retention-gap window was skipped in the last 15 minutes; that window's data is permanently lost.", 405,
     'sum(increase(cf2otel_window_gap_seconds_total{service_name="cf2otel"}[15m])) or on() (0 * sum(increase(cf2otel_scrape_success_total{service_name="cf2otel"}[15m])))', 0, "Alerting"),
    ("cf2otel-window-commit-dropped", "cf2otel window commit dropped", "A window's commit failed permanently (dropped outcome) in the last 15 minutes after repeated payload rejection.", 406,
     'sum(increase(cf2otel_window_commit_failures_total{service_name="cf2otel",outcome="dropped"}[15m])) or on() (0 * sum(increase(cf2otel_scrape_success_total{service_name="cf2otel"}[15m])))', 0, "Alerting"),
    ("cf2otel-access-checkpoint-age", "Access logins checkpoint age exceeds 12 hours", "Access logins checkpoint age is over 12 hours, approaching the Access REST log's roughly one-day reach; further delay risks permanent data loss.", 404,
     'max(cf2otel_checkpoint_age_seconds{service_name="cf2otel",cf2otel_collector="access.logins"})', 43200, "Alerting"),
    ("cf2otel-logpush-final-failure", "Cloudflare Logpush final upload attempt failed", "Final-attempt upload failures with destination status >=300 increased or were first observed in the last 15 minutes, grouped by scope, zone, job and destination. First-seen positive counters include startup/backfill observations. Requires opt-in logpush.failures; absent data is not proof of successful delivery.", 2650,
     f'sum by (cloudflare_logpush_scope, cloudflare_logpush_zone, cloudflare_logpush_job_id, cloudflare_logpush_destination_type) ({LOGPUSH_FINAL_FAILURE})', 0, "NoData"),
]


def resource(name: str, title: str, description: str, panel_id: int, expr: str, threshold: int, no_data_state: str) -> dict:
    if f"panel-{panel_id}" not in render()["spec"]["elements"]:
        raise ValueError(f"missing dashboard panel {panel_id}")
    return {"apiVersion": "rules.alerting.grafana.app/v0alpha1", "kind": "AlertRule",
        "metadata": {"name": name, "annotations": {"grafana.app/folder": FOLDER}, "labels": {"grafana.app/folder": FOLDER}},
        "spec": {"title": title, "trigger": {"interval": "1m"}, "for": "5m", "paused": False,
        "noDataState": no_data_state, "execErrState": "Error", "labels": {"category": "collector", "pipeline": "cf2otel", "service": "cf2otel", "severity": "warning", "source": "cf2otel"},
        "annotations": {"__dashboardUid__": "cf2otel", "__panelId__": str(panel_id), "summary": title,
            "description": description, "runbook_url": "https://github.com/rknightion/cf2otel/blob/main/docs/troubleshooting.md"},
        "expressions": {"A": {"datasourceUID": PROM, "queryType": "instant", "relativeTimeRange": {"from": "15m", "to": "0s"},
            "model": {"refId": "A", "expr": expr, "instant": True, "range": False, "datasource": {"type": "prometheus", "uid": PROM}}},
            "B": {"datasourceUID": "__expr__", "model": {"refId": "B", "type": "reduce", "expression": "A", "reducer": "last", "datasource": {"type": "__expr__", "uid": "__expr__"}}},
            "C": {"source": True, "datasourceUID": "__expr__", "model": {"refId": "C", "type": "threshold", "expression": "B", "datasource": {"type": "__expr__", "uid": "__expr__"},
                "conditions": [{"evaluator": {"type": "gt", "params": [threshold]}}]}}},
        "panelRef": {"dashboardUID": "cf2otel", "panelID": panel_id}}}


LOKI = "grafanacloud-logs"
# jev's gateway log records report zero usage, so its input tokens come from the
# captured response body; every other model uses the gateway's own count.
DAILY_INPUT_TOKENS = (
    '(sum(sum_over_time({service_name="cf2otel"} | event_name="gen_ai.client.inference.operation.details" '
    '| gen_ai_request_model="jev" | json in="result.usage.input_tokens" | unwrap in [24h])) or vector(0)) + '
    '(sum(sum_over_time({service_name="cf2otel"} | event_name="cloudflare.ai_gateway.request" '
    '| gen_ai_request_model!="jev" | unwrap gen_ai_usage_input_tokens [24h])) or vector(0))')
DAILY_INPUT_TOKENS_THRESHOLD = 150_000_000


def daily_input_tokens_rule() -> dict:
    """AI Gateway spend alert: a Loki rule outside the collector-health shape.

    No `for` and Ok error states match the rule as first created; the verify
    step treats an added `for: 0s` as drift from the live rule.
    """
    expr_ds = {"type": "__expr__", "uid": "__expr__"}
    return {"apiVersion": "rules.alerting.grafana.app/v0alpha1", "kind": "AlertRule",
        "metadata": {"name": "ai-gateway-daily-input-tokens", "annotations": {"grafana.app/folder": FOLDER}, "labels": {"grafana.app/folder": FOLDER}},
        "spec": {"title": "AIGatewayDailyInputTokensHigh", "trigger": {"interval": "15m"}, "paused": False,
        "noDataState": "Ok", "execErrState": "Ok",
        "labels": {"category": "spend", "domain": "observability", "page": "false", "service": "cf2otel", "severity": "warning", "source": "cf2otel"},
        "annotations": {"summary": "AI Gateway input tokens over the last 24h: {{ humanize $values.B.Value }} (threshold 150M)",
            "description": "Rolling 24h input tokens through the Cloudflare AI Gateway. jevgrep's jev model is counted from the response body, "
                           "because its gateway log records report 0 usage (cf2otel CFO-0086); every other model uses gen_ai.usage.input_tokens. "
                           "Normal days are 15-40M. The 2026-10-19 spike was 355M, almost all from jg calls in loop lanes. "
                           "Check which loops are running jg heavily, using the agent-history catalogue."},
        "expressions": {"A": {"datasourceUID": LOKI, "queryType": "instant", "relativeTimeRange": {"from": "24h", "to": "0s"},
            "model": {"refId": "A", "expr": DAILY_INPUT_TOKENS, "queryType": "instant", "datasource": {"type": "loki", "uid": LOKI}}},
            "B": {"datasourceUID": "__expr__", "model": {"refId": "B", "type": "reduce", "expression": "A", "reducer": "last", "datasource": expr_ds}},
            "C": {"source": True, "datasourceUID": "__expr__", "model": {"refId": "C", "type": "threshold", "expression": "B", "datasource": expr_ds,
                "conditions": [{"evaluator": {"type": "gt", "params": [DAILY_INPUT_TOKENS_THRESHOLD]}}]}}}}}


def certificate_fixtures() -> dict:
    """Promtool cases exercise generated queries against atomic snapshot exports.

    Expectations come from the alert contract, not the generated expression.
    Identity labels are opaque, and both gauges retire in the same export.
    """
    expressions = {r[0]: r[4] for r in RULES}
    identity = '{cloudflare_certificate_pack_id="pack-demo",cloudflare_certificate_zone="zone-demo"}'
    labels = 'service_name="cf2otel",cloudflare_certificate_zone="zone-demo",cloudflare_certificate_pack_id="pack-demo"'
    tests = []
    cases = [
        # name, expiry, status, age, expected expiry/status scores
        ("under fourteen days", 1209599, "active", 0, 1, 0),
        ("exactly fourteen days", 1209600, "active", 0, 0, 0),
        ("expired", -1, "active", 0, 1, 0),
        ("pending unknown expiry", None, "pending", 0, None, 1),
        # backup_issued is Cloudflare's normal standby (second-CA) state and must not alert.
        ("backup issued standby", 2000000, "backup_issued", 0, 0, 0),
        # A real not-yet-valid state must still alert.
        ("pending validation", 2000000, "pending_validation", 0, 0, 1),
        ("fresh just inside three intervals", 100, "pending", CERTS_PACKS_FRESHNESS_SECONDS - 1, 1, 1),
        ("stale at three intervals", 100, "pending", CERTS_PACKS_FRESHNESS_SECONDS, None, None),
        ("stale beyond three intervals", 100, "pending", CERTS_PACKS_FRESHNESS_SECONDS + 1, None, None),
    ]
    for name, expiry, status, age, expiry_score, status_score in cases:
        series = [{"series": f'cloudflare_certificate_pack{{{labels},cloudflare_certificate_status="{status}"}}', "values": "1 1"},
                  {"series": 'cf2otel_scrape_last_success_timestamp_seconds{service_name="cf2otel",cf2otel_collector="certs.packs"}', "values": f'{300 - age} {300 - age}'}]
        if expiry is not None:
            series.append({"series": f'cloudflare_certificate_expiry_seconds{{{labels},cloudflare_certificate_status="{status}"}}', "values": f'{expiry} {expiry}'})
        checks = []
        for suffix, score in (("expiry", expiry_score), ("status", status_score)):
            checks.append({"expr": expressions[f"cf2otel-certificate-{suffix}"], "eval_time": "5m",
                           "exp_samples": [] if score is None else [{"labels": identity, "value": score}]})
        tests.append({"name": name, "interval": "5m", "input_series": series, "promql_expr_test": checks})
    # Old pending/expiring series disappear, rather than remaining as freshly
    # exported synchronous gauges. Include both a healthy replacement and empty inventory.
    for replacement in (True, False):
        series = [{"series": f'{metric}{{{labels},cloudflare_certificate_status="pending"}}', "values": f'{value} stale'}
                  for metric, value in (("cloudflare_certificate_pack", 1), ("cloudflare_certificate_expiry_seconds", 100))]
        series.append({"series": 'cf2otel_scrape_last_success_timestamp_seconds{service_name="cf2otel",cf2otel_collector="certs.packs"}', "values": "0 60"})
        if replacement:
            series.extend({"series": f'{metric}{{{labels},cloudflare_certificate_status="active"}}', "values": f'_ {value}'}
                          for metric, value in (("cloudflare_certificate_pack", 1), ("cloudflare_certificate_expiry_seconds", 2000000)))
        checks = [{"expr": expressions[f"cf2otel-certificate-{suffix}"], "eval_time": when,
                   "exp_samples": [{"labels": identity, "value": score}] if score is not None else []}
                  for suffix in ("expiry", "status") for when, score in (("0s", 1), ("1m", 0 if replacement else None))]
        tests.append({"name": "healthy replacement" if replacement else "empty snapshot retirement", "interval": "1m", "input_series": series, "promql_expr_test": checks})
    # A fresh exporter must not resurrect a stale exporter's pending pack.
    mixed = []
    for instance, status, expiry, timestamp in (("stale-demo", "pending", 100, -CERTS_PACKS_FRESHNESS_SECONDS), ("fresh-demo", "active", 2000000, 0)):
        mixed.extend({"series": f'{metric}{{{labels},instance="{instance}",cloudflare_certificate_status="{status}"}}', "values": str(value)}
                     for metric, value in (("cloudflare_certificate_pack", 1), ("cloudflare_certificate_expiry_seconds", expiry)))
        mixed.append({"series": f'cf2otel_scrape_last_success_timestamp_seconds{{service_name="cf2otel",cf2otel_collector="certs.packs",instance="{instance}"}}', "values": str(timestamp)})
    tests.append({"name": "stale exporter alongside fresh exporter", "interval": "1m", "input_series": mixed,
                  "promql_expr_test": [{"expr": expressions[f"cf2otel-certificate-{suffix}"], "eval_time": "0s",
                                        "exp_samples": [{"labels": identity, "value": 0}]} for suffix in ("expiry", "status")]})
    # Presence without a last-success signal must never prove freshness.
    tests.append({"name": "absent collector freshness", "interval": "1m",
                  "input_series": [{"series": f'{metric}{{{labels},cloudflare_certificate_status="pending"}}', "values": str(value)}
                                   for metric, value in (("cloudflare_certificate_pack", 1), ("cloudflare_certificate_expiry_seconds", 100))],
                  "promql_expr_test": [{"expr": expressions[f"cf2otel-certificate-{suffix}"], "eval_time": "0s", "exp_samples": []}
                                       for suffix in ("expiry", "status")]})
    return {"rule_files": [], "evaluation_interval": "1m", "tests": tests}


def collector_stale_fixtures() -> dict:
    """Promtool cases for per-collector staleness thresholds.

    The rule value is seconds beyond three polling intervals and the alert
    condition is value > 0, so expectations are exact margins from the contract:
    default 300s collectors fire after 900s, hourly collectors after 10800s.
    """
    expression = {r[0]: r[4] for r in RULES}["cf2otel-collector-stale"]
    hourly = ("certs.packs", "httpreq.threats", "httpreq.transfer")
    tests = []

    def case(name: str, collector: str, age: int, threshold: int) -> None:
        tests.append({"name": name, "interval": "1m", "input_series": [
            {"series": f'cf2otel_scrape_last_success_timestamp_seconds{{service_name="cf2otel",cf2otel_collector="{collector}"}}',
             "values": f"{300 - age} {300 - age}"}],
            "promql_expr_test": [{"expr": expression, "eval_time": "5m",
                                  "exp_samples": [{"labels": f'{{cf2otel_collector="{collector}"}}', "value": age - threshold}]}]})

    for collector in hourly:
        case(f"{collector} hourly collector 1200s old does not fire", collector, 1200, 10800)
        case(f"{collector} hourly collector at three intervals does not fire", collector, 10800, 10800)
        case(f"{collector} hourly collector beyond three intervals fires", collector, 10801, 10800)
        case(f"{collector} hourly collector long stale fires", collector, 20000, 10800)
    for age, verb in ((900, "does not fire"), (901, "fires"), (1200, "fires")):
        case(f"five-minute collector {age}s old {verb}", "access.logins", age, 900)
    case("access.seats fifteen-minute collector 1200s old does not fire", "access.seats", 1200, 3 * ACCESS_SEATS_INTERVAL_SECONDS)
    case("access.seats fifteen-minute collector beyond three intervals fires", "access.seats", 3 * ACCESS_SEATS_INTERVAL_SECONDS + 1, 3 * ACCESS_SEATS_INTERVAL_SECONDS)
    # Classes must not bleed into each other when several collectors report together.
    tests.append({"name": "mixed hourly and five-minute collectors keep their own thresholds", "interval": "1m", "input_series": [
        {"series": f'cf2otel_scrape_last_success_timestamp_seconds{{service_name="cf2otel",cf2otel_collector="{c}"}}', "values": f"{300 - a} {300 - a}"}
        for c, a in (("certs.packs", 1200), ("httpreq.threats", 11000), ("access.logins", 1200), ("firewall.events", 100))],
        "promql_expr_test": [{"expr": expression, "eval_time": "5m", "exp_samples": [
            {"labels": f'{{cf2otel_collector="{c}"}}', "value": v}
            for c, v in (("certs.packs", 1200 - 10800), ("httpreq.threats", 11000 - 10800), ("access.logins", 300), ("firewall.events", 100 - 900))]}]})
    return {"rule_files": [], "evaluation_interval": "1m", "tests": tests}


def polling_health_fixtures() -> dict:
    """Runtime intervals and adjusted backlog define the alert scores; a backlog over
    BACKLOG_INTERVALS removes that collector's slow-poll score."""
    tests = []
    rules = {r[0]: r for r in RULES}
    expressions = {"scrape_elapsed": rules["cf2otel-collector-slow"][4],
                   "checkpoint_backlog": rules["cf2otel-checkpoint-backlog"][4]}

    def case(name, rows, expected):
        # Rows: instance, collector, interval seconds, elapsed seconds, backlog
        # seconds (None means a snapshot collector, which has no checkpoint).
        series = []
        for instance, collector, interval, elapsed, backlog in rows:
            labels = f'service_name="cf2otel",instance="{instance}",cf2otel_collector="{collector}"'
            for metric, value in (("scrape_interval", interval), ("scrape_elapsed", elapsed), ("checkpoint_backlog", backlog)):
                if value is not None:
                    series.append({"series": f'cf2otel_{metric}_seconds{{{labels}}}', "values": str(value)})
        tests.append({"name": name, "interval": "1m", "input_series": series,
                      "promql_expr_test": [
                          {"expr": expressions[metric], "eval_time": "0s",
                           "exp_samples": [{"labels": f'{{cf2otel_collector="{collector}"}}', "value": score}
                                           for collector, score in scores]}
                          for metric, scores in zip(("scrape_elapsed", "checkpoint_backlog"), expected)]})

    case("healthy collector retains zero scores", [("one", "aigateway.logs", 300, 0, 0)],
         ([("aigateway.logs", 0)], [("aigateway.logs", 0)]))
    case("active slow poll crosses its interval", [("one", "aigateway.logs", 300, 600, 0)],
         ([("aigateway.logs", 2)], [("aigateway.logs", 0)]))
    case("threshold boundaries stay healthy", [("one", "dns.metrics", 300, 300, 900)],
         ([("dns.metrics", 1)], [("dns.metrics", 3)]))
    case("successful short pulls can leave a large backlog", [("one", "aigateway.logs", 300, 30, 1800)],
         ([], [("aigateway.logs", 6)]))
    case("slow catch-up poll is left to the backlog rule", [("one", "aigateway.logs", 300, 600, 1800)],
         ([], [("aigateway.logs", 6)]))
    case("slow poll at the backlog threshold still scores", [("one", "aigateway.logs", 300, 600, 900)],
         ([("aigateway.logs", 2)], [("aigateway.logs", 3)]))
    case("backlog on one collector leaves another slow collector scored", [
        ("one", "aigateway.logs", 300, 600, 1800),
        ("one", "dns.metrics", 300, 600, 0)],
         ([("dns.metrics", 2)], [("aigateway.logs", 6), ("dns.metrics", 0)]))
    case("hourly collector uses runtime interval and adjusted backlog", [("one", "httpreq.threats", 3600, 900, 3600)],
         ([("httpreq.threats", 0.25)], [("httpreq.threats", 1)]))
    case("snapshot has polling score but no backlog", [("one", "certs.packs", 3600, 30, None)],
         ([("certs.packs", 1 / 120)], []))
    case("disabled collectors and missing telemetry stay absent", [], ([], []))
    case("missing interval is not healthy zero", [("one", "dns.metrics", None, 600, 1800)], ([], []))
    case("invalid zero interval is excluded", [("one", "dns.metrics", 0, 600, 1800)], ([], []))
    case("normalize each replica before worst score reduction", [
        ("slow", "dns.metrics", 300, 600, 600),
        ("hourly", "dns.metrics", 3600, 900, 3600)],
         ([("dns.metrics", 2)], [("dns.metrics", 2)]))
    return {"rule_files": [], "evaluation_interval": "1m", "tests": tests}


def dashboard_panel_fixtures() -> dict:
    """Evaluate the shipped panel queries locally; no live data or replicas summed.

    Replace Grafana macros with a five-minute fixture window and the All zone
    filter. Expected values follow gauge/counter semantics, not panel snapshots.
    """
    panels = render()["spec"]["elements"]

    def expr(pid: int, query: int = 0) -> str:
        return (panels[f"panel-{pid}"]["spec"]["data"]["spec"]["queries"][query]
                ["spec"]["query"]["spec"]["expr"]
                .replace("$__range", "5m").replace("$__interval", "5m").replace("$zone", ".*"))

    zone_series = []
    zone_checks = []
    for index, stage in enumerate(("discovered", "filtered", "processed", "skipped")):
        reason = ',cf2otel_zone_reason="exclude"' if stage == "filtered" else (
            ',cf2otel_zone_reason="unentitled"' if stage == "skipped" else "")
        value = (8, 2, 6, 0)[index]
        for instance in ("fresh-fixture", "replica-fixture", "old-fixture"):
            labels = f'service_name="cf2otel",instance="{instance}",cf2otel_collector="fixture-collector"{reason}'
            zone_series.append({"series": f'cf2otel_zones_{stage}{{{labels}}}', "values": f'{value}+0x5'})
        zone_checks.append({"expr": expr(2715, index), "eval_time": "5m", "exp_samples": [
            {"labels": f'{{instance="{instance}",cf2otel_collector="fixture-collector"{reason}}}', "value": value}
            for instance in ("fresh-fixture", "replica-fixture")]})
    for instance, timestamp in (("fresh-fixture", 300), ("replica-fixture", 300), ("old-fixture", -900)):
        zone_series.append({"series": f'cf2otel_scrape_last_success_timestamp_seconds{{service_name="cf2otel",instance="{instance}",cf2otel_collector="fixture-collector"}}',
                            "values": f'{timestamp}+0x5'})
    # A gauge with no last-success witness is not considered fresh.
    zone_series.append({"series": 'cf2otel_zones_discovered{service_name="cf2otel",instance="unknown-fixture",cf2otel_collector="fixture-collector"}',
                        "values": '99+0x5'})

    error_labels = 'instance="fresh-fixture",cf2otel_collector="fixture-collector"'
    errors = [{"series": f'cf2otel_scrape_errors_total{{service_name="cf2otel",{error_labels},cf2otel_error_class="{kind}"}}',
               "values": values} for kind, values in (("auth", "0+1x5"), ("timeout", "0+2x5"), ("other", "0+0x5"))]
    firewall_labels = ('instance="fresh-fixture",cloudflare_firewall_zone="fixture-zone",'
                       'cloudflare_firewall_rule_id="fixture-rule",cloudflare_firewall_rule_description="Fixture rule",'
                       'cloudflare_firewall_host="fixture-host",cloudflare_firewall_client_country="GB",'
                       'cloudflare_firewall_action="block",cloudflare_firewall_source="fixture-engine"')
    fallback_labels = 'instance="fresh-fixture",cloudflare_firewall_zone="fixture-fallback"'
    return {"rule_files": [], "evaluation_interval": "1m", "tests": [
        {"name": "zone gauges preserve reasons zero and independent fresh instances", "interval": "1m",
         "input_series": zone_series, "promql_expr_test": zone_checks},
        {"name": "error classes remain independent including seeded zero", "interval": "1m", "input_series": errors,
         "promql_expr_test": [{"expr": expr(2714), "eval_time": "5m", "exp_samples": [
             {"labels": f'{{{error_labels},cf2otel_error_class="{kind}"}}', "value": value}
             for kind, value in (("auth", 5), ("timeout", 10), ("other", 0))]}]},
        {"name": "absent never incremented errors are not manufactured", "interval": "1m", "input_series": [],
         "promql_expr_test": [{"expr": expr(2714), "eval_time": "5m", "exp_samples": []}]},
        {"name": "firewall metric retains enrichment and unadvertised fallback", "interval": "1m",
         "input_series": [{"series": f'cloudflare_firewall_events_total{{service_name="cf2otel",{labels}}}', "values": values}
                          for labels, values in ((firewall_labels, "0+2x5"), (fallback_labels, "0+1x5"))],
         "promql_expr_test": [{"expr": expr(2116), "eval_time": "5m", "exp_samples": [
             {"labels": '{' + firewall_labels + '}', "value": 10},
             {"labels": '{' + fallback_labels + '}', "value": 5}]}]},
    ]}


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    artifacts = [(OUT / f"{rule[0]}.json", resource(*rule)) for rule in RULES]
    artifacts.append((OUT / "ai-gateway-daily-input-tokens.json", daily_input_tokens_rule()))
    artifacts.append((OUT / "fixtures" / "certificates.test.yaml", certificate_fixtures()))
    artifacts.append((OUT / "fixtures" / "collector-stale.test.yaml", collector_stale_fixtures()))
    artifacts.append((OUT / "fixtures" / "polling-health.test.yaml", polling_health_fixtures()))
    artifacts.append((OUT / "fixtures" / "dashboard-panels.test.yaml", dashboard_panel_fixtures()))
    for path, artifact in artifacts:
        content = json.dumps(artifact, indent=2, sort_keys=True) + "\n"
        if args.check:
            if not path.exists() or path.read_text(encoding="utf-8") != content:
                raise SystemExit(f"generated rule drift: {path.relative_to(ROOT)}")
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content, encoding="utf-8")


if __name__ == "__main__":
    main()
