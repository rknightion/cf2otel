#!/usr/bin/env python3
"""Generate cf2otel Grafana-managed alert rules."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path

from build_dashboard import CERT_FRESH, CERTS_PACKS_FRESHNESS_SECONDS, CERTS_PACKS_INTERVAL_SECONDS, render

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "alerts" / "grafana-managed"
FOLDER = "REPLACE_WITH_FOLDER_UID"
PROM = "grafanacloud-prom"


def tunnels_status_interval_seconds() -> int:
    """Read the deployment-aligned generator setting, not a runtime config key."""
    key = "GRAFANA_TUNNELS_STATUS_INTERVAL_SECONDS"
    value = os.environ.get(key, "60")
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
CERT_STATUS = f'(max by ({CERT_ID}) ((cloudflare_certificate_pack{{service_name="cf2otel",cloudflare_certificate_status!="active"}} == 1) and on(instance) {CERT_FRESH}) or on ({CERT_ID}) (0 * max by ({CERT_ID}) (({CERT_PACK} == 1) and on(instance) {CERT_FRESH})))'
CERT_DESCRIPTION = (f"Requires opt-in certs.packs and collector last success less than {CERTS_PACKS_FRESHNESS_SECONDS} seconds old "
                    f"(three deployment polling intervals of {CERTS_PACKS_INTERVAL_SECONDS} seconds). Removed packs and previous statuses retire. "
                    "Absent/stale data is not proof of health. For a non-default interval regenerate with GRAFANA_CERTS_PACKS_INTERVAL_SECONDS matching the deployment.")
RULES = [
    ("cf2otel-certificate-expiry", "Cloudflare certificate expires in under 14 days", "Earliest observed expiry in a present pack is under 14 days, including already expired packs. Seconds to expiry are observed at the last successful poll, not a live countdown. " + CERT_DESCRIPTION, 2640,
     CERT_EXPIRY, 0, "NoData"),
    ("cf2otel-certificate-status", "Cloudflare certificate pack is not active", "A present certificate pack has a non-active status, including packs with unknown expiry. " + CERT_DESCRIPTION, 2645,
     CERT_STATUS, 0, "NoData"),
    ("cf2otel-tunnel-unhealthy", "Cloudflare tunnel is not healthy", f"The current (value 1) tunnel status has been non-healthy for five minutes, with collector last success less than {TUNNELS_STATUS_FRESHNESS_SECONDS} seconds old (three deployment polling intervals of {TUNNELS_STATUS_INTERVAL_SECONDS} seconds). Retired value-0 states are excluded. Collector is disabled by default; stale/absent data is not proof of health.", 2641,
     '(max by (cloudflare_tunnel_id, cloudflare_tunnel_name) (cloudflare_tunnel_status{service_name="cf2otel",cloudflare_tunnel_status!="healthy"} == 1) or on (cloudflare_tunnel_id, cloudflare_tunnel_name) (0 * max by (cloudflare_tunnel_id, cloudflare_tunnel_name) (cloudflare_tunnel_status{service_name="cf2otel"} == 1))) and on() (time() - max(cf2otel_scrape_last_success_timestamp_seconds{service_name="cf2otel",cf2otel_collector="tunnels.status"}) < ' + str(TUNNELS_STATUS_FRESHNESS_SECONDS) + ')', 0, "NoData"),
    ("cf2otel-collector-stale", "cf2otel collector is stale", "Collector last-success timestamp is older than 15 minutes.", 401,
     'time() - max by (cf2otel_collector) (cf2otel_scrape_last_success_timestamp_seconds{service_name="cf2otel"})', 900, "Alerting"),
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
    artifacts.append((OUT / "fixtures" / "certificates.test.yaml", certificate_fixtures()))
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
