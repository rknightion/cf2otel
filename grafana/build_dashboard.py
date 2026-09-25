#!/usr/bin/env python3
"""Generate the cf2otel Grafana Dashboard v2 resource."""
from __future__ import annotations

import argparse
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "dashboards" / "cf2otel.json"
PROM = "${ds_prometheus}"
LOKI = "${ds_loki}"
SVC = 'service_name="cf2otel"'


def datasource(name: str, plugin: str, default: str) -> dict:
    return {"kind": "DatasourceVariable", "spec": {"name": name, "label": plugin.title(), "pluginId": plugin,
        "current": {"text": default, "value": default}, "options": [], "multi": False, "includeAll": False,
        "allowCustomValue": True, "hide": "dontHide", "refresh": "onDashboardLoad", "regex": "", "skipUrlSync": False}}


def query(ds: str, expr: str, *, ref: str = "A", logs: bool = False, instant: bool = False) -> dict:
    return {"kind": "PanelQuery", "spec": {"refId": ref, "hidden": False, "datasource": {"uid": ds}, "query": {
        "kind": "DataQuery", "version": "v0", "group": "loki" if ds == LOKI else "prometheus",
        "datasource": {"name": ds}, "spec": {"expr": expr, "refId": ref, "instant": instant,
        "range": not instant, "legendFormat": "", "format": "logs" if logs else "time_series"}}}}


def panel(pid: int, title: str, description: str, ds: str, expr: str, *, viz: str = "timeseries", unit: str = "short", logs: bool = False, instant: bool = False, second: str | None = None) -> tuple[str, dict]:
    queries = [query(ds, expr, logs=logs, instant=instant)]
    if second:
        queries.append(query(ds, second, ref="B", instant=instant))
    return f"panel-{pid}", {"kind": "Panel", "spec": {"id": pid, "title": title, "description": description,
        "links": [], "data": {"kind": "QueryGroup", "spec": {"queries": queries, "queryOptions": {}, "transformations": []}},
        "vizConfig": {"kind": "VizConfig", "group": viz, "version": "12.1.0",
        "spec": {"options": {}, "fieldConfig": {"defaults": {"unit": unit}, "overrides": []}}}}}


def row(title: str, ids: list[int], *, collapse: bool = False) -> dict:
    items = []
    for i, pid in enumerate(ids):
        items.append({"kind": "GridLayoutItem", "spec": {"x": i % 2 * 12, "y": i // 2 * 8,
            "width": 12, "height": 8, "element": {"kind": "ElementReference", "name": f"panel-{pid}"}}})
    return {"kind": "RowsLayoutRow", "spec": {"title": title, "collapse": collapse,
        "layout": {"kind": "GridLayout", "spec": {"items": items}}}}


def tab(title: str, ids: list[int]) -> dict:
    return {"kind": "TabsLayoutTab", "spec": {"title": title,
        "layout": {"kind": "RowsLayout", "spec": {"rows": [row(title, ids)]}}}}


def platform_tab() -> dict:
    families = [
        ("Workers, Turnstile and Logpush", list(range(501, 505))),
        ("D1 and KV", list(range(601, 608))),
        ("R2", list(range(701, 709))),
        ("Durable Objects", list(range(801, 805))),
        ("Queues", list(range(901, 907))),
    ]
    return {"kind": "TabsLayoutTab", "spec": {"title": "Platform analytics",
        "layout": {"kind": "RowsLayout", "spec": {"rows": [
            row(title, ids, collapse=True) for title, ids in families]}}}}


def render() -> dict:
    p = dict([
        panel(101, "Human Access logins", "Exact REST login events in the selected range. Service-token traffic is excluded by source.", LOKI,
              'sum(count_over_time({service_name="cf2otel"} | event_name="cloudflare.access.login" [$__range]))', viz="stat", instant=True),
        panel(102, "Nonidentity Access requests", "Separate service-token and bypass traffic; GraphQL Groups count is corrected for sampling.", PROM,
              'sum by (cloudflare_access_service_token) (rate(cloudflare_access_requests_total{service_name="cf2otel",cloudflare_access_identity_provider="nonidentity"}[$__rate_interval]))', unit="reqps"),
        panel(103, "Access logins by app and outcome", "Access GraphQL identity login metrics by app and allowed state; REST log counters are excluded to avoid double counting.", PROM,
              'sum by (cloudflare_access_app, cloudflare_access_allowed) (rate(cloudflare_access_logins_total{service_name="cf2otel",cloudflare_access_identity_provider!="",cloudflare_access_identity_provider!="nonidentity"}[$__rate_interval]))', unit="reqps"),
        panel(201, "Protected-host HTTP requests", "Corrected request rate from httpRequestsAdaptiveGroups, not sampled raw events.", PROM,
              'sum by (cloudflare_http_host, cf2otel_status_class) (rate(cloudflare_http_requests_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(202, "Inferred identity events", "Log events whose Access user match was inferred from IP, host and time; this is not authenticated request identity.", LOKI,
              'sum(count_over_time({service_name="cf2otel"} | event_name="cloudflare.http.request" | cloudflare_access_identity_inferred="true" [$__range]))', viz="stat", instant=True),
        panel(203, "Protected-host HTTP events", "Sampled per-request events. User identity, when present, is explicitly inferred.", LOKI,
              '{service_name="cf2otel"} | event_name="cloudflare.http.request"', viz="logs", logs=True),
        panel(301, "AI Gateway requests and errors", "REST-log request counters; errors are a separate series.", PROM,
              'sum(rate(cloudflare_ai_gateway_requests_total{service_name="cf2otel"}[$__rate_interval]))',
              second='sum(rate(cloudflare_ai_gateway_errors_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(302, "AI Gateway cost", "Gateway-reported cost values; currency is unverified, so no currency unit is asserted.", PROM,
              'sum(increase(cloudflare_ai_gateway_cost_total{service_name="cf2otel"}[$__range]))', viz="stat", instant=True, unit="none"),
        panel(303, "AI Gateway p95 latency", "Completed request duration from REST rows, converted to seconds.", PROM,
              'histogram_quantile(0.95, sum by (le, gen_ai_request_model) (rate(gen_ai_client_operation_duration_bucket{service_name="cf2otel"}[$__rate_interval])))', unit="s"),
        panel(304, "AI Gateway token usage", "Input and output totals are separate; cached and reasoning subsets are not added to totals.", PROM,
              'sum(rate(gen_ai_client_inference_usage_input_tokens_total{service_name="cf2otel"}[$__rate_interval]))',
              second='sum(rate(gen_ai_client_inference_usage_output_tokens_total{service_name="cf2otel"}[$__rate_interval]))', unit="short"),
        panel(401, "Collector last-success age", "Seconds since each collector last succeeded. An absent series needs investigation too.", PROM,
              'time() - max by (cf2otel_collector) (cf2otel_scrape_last_success_timestamp{service_name="cf2otel"})', unit="s"),
        panel(402, "Collector errors", "Errors over the selected range, by collector.", PROM,
              'sum by (cf2otel_collector) (increase(cf2otel_scrape_errors_total{service_name="cf2otel"}[$__range]))', unit="short"),
        panel(403, "Export failures", "Export failures over the selected range; zero is healthy if the exporter reports the series.", PROM,
              'sum(increase(cf2otel_export_errors_total{service_name="cf2otel"}[$__range]))', viz="stat", instant=True),
        panel(404, "Checkpoint age", "Age of each collector checkpoint; compare with its configured interval.", PROM,
              'max by (cf2otel_collector) (cf2otel_checkpoint_age{service_name="cf2otel"})', unit="s"),
        panel(501, "Workers requests by script", "Request rate from workersOverviewRequestsAdaptiveGroups, grouped by the bounded script name.", PROM,
              'sum by (cloudflare_workers_script_name) (rate(cloudflare_workers_requests_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(502, "Turnstile events", "Event rate from turnstileAdaptiveGroups.", PROM,
              'sum(rate(cloudflare_turnstile_events_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(503, "Logpush uploads", "Upload rate from logpushHealthAdaptiveGroups.", PROM,
              'sum(rate(cloudflare_logpush_uploads_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(504, "Logpush records", "Record rate from logpushHealthAdaptiveGroups.", PROM,
              'sum(rate(cloudflare_logpush_records_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(601, "D1 read queries", "Read query rate from d1AnalyticsAdaptiveGroups.", PROM,
              'sum(rate(cloudflare_d1_read_queries_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(602, "D1 write queries", "Write query rate from d1AnalyticsAdaptiveGroups.", PROM,
              'sum(rate(cloudflare_d1_write_queries_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(603, "D1 queries", "Query rate from d1QueriesAdaptiveGroups; query text is never selected or emitted.", PROM,
              'sum(rate(cloudflare_d1_queries_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(604, "D1 storage", "Maximum database size from d1StorageAdaptiveGroups.", PROM,
              'max(cloudflare_d1_storage_max_database_bytes{service_name="cf2otel"})', unit="bytes"),
        panel(605, "KV requests", "Request rate from kvOperationsAdaptiveGroups.", PROM,
              'sum(rate(cloudflare_kv_requests_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(606, "KV storage bytes", "Maximum namespace byte count from kvStorageAdaptiveGroups.", PROM,
              'max(cloudflare_kv_storage_max_namespace_bytes{service_name="cf2otel"})', unit="bytes"),
        panel(607, "KV storage keys", "Maximum namespace key count from kvStorageAdaptiveGroups.", PROM,
              'max(cloudflare_kv_storage_max_namespace_keys{service_name="cf2otel"})'),
        panel(701, "R2 download bandwidth", "Download byte rate from r2BandwidthUsageAdaptiveGroups, grouped by bucket.", PROM,
              'sum by (cloudflare_r2_bucket_name) (rate(cloudflare_r2_bandwidth_download_bytes_total{service_name="cf2otel"}[$__rate_interval]))', unit="Bps"),
        panel(702, "R2 upload bandwidth", "Upload byte rate from r2BandwidthUsageAdaptiveGroups, grouped by bucket.", PROM,
              'sum by (cloudflare_r2_bucket_name) (rate(cloudflare_r2_bandwidth_upload_bytes_total{service_name="cf2otel"}[$__rate_interval]))', unit="Bps"),
        panel(703, "R2 catalog data operations", "Operation rate from r2CatalogDataOperationsAdaptiveGroups, grouped by namespace.", PROM,
              'sum by (cloudflare_r2_catalog_namespace_name) (rate(cloudflare_r2_catalog_data_operations_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(704, "R2 catalog maintenance jobs", "Job rate from r2CatalogTableMaintenanceAdaptiveGroups, grouped by namespace.", PROM,
              'sum by (cloudflare_r2_catalog_namespace_name) (rate(cloudflare_r2_catalog_maintenance_jobs_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(705, "R2 requests by bucket", "Request rate from r2OperationsAdaptiveGroups, grouped by bucket.", PROM,
              'sum by (cloudflare_r2_bucket_name) (rate(cloudflare_r2_requests_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(706, "R2 storage payload", "Maximum payload bytes from r2StorageAdaptiveGroups, grouped by bucket.", PROM,
              'max by (cloudflare_r2_bucket_name) (cloudflare_r2_storage_payload_bytes{service_name="cf2otel"})', unit="bytes"),
        panel(707, "R2 storage objects", "Maximum object count from r2StorageAdaptiveGroups, grouped by bucket.", PROM,
              'max by (cloudflare_r2_bucket_name) (cloudflare_r2_storage_objects{service_name="cf2otel"})'),
        panel(708, "R2 SQL queries", "Query rate from r2sqlOperationsAdaptiveGroups, grouped by bucket.", PROM,
              'sum by (cloudflare_r2sql_bucket_name) (rate(cloudflare_r2sql_queries_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(801, "Durable Objects requests", "Request rate from durableObjectsInvocationsAdaptiveGroups.", PROM,
              'sum(rate(cloudflare_durableobjects_requests_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(802, "Durable Objects subrequests", "Subrequest rate from durableObjectsPeriodicGroups.", PROM,
              'sum(rate(cloudflare_durableobjects_subrequests_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(803, "Durable Objects SQL storage", "Maximum stored bytes from durableObjectsSqlStorageGroups.", PROM,
              'max(cloudflare_durableobjects_sql_storage_max_namespace_bytes{service_name="cf2otel"})', unit="bytes"),
        panel(804, "Durable Objects request body bytes", "Uncached request body byte rate from durableObjectsSubrequestsAdaptiveGroups.", PROM,
              'sum(rate(cloudflare_durableobjects_subrequests_request_body_bytes_total{service_name="cf2otel"}[$__rate_interval]))', unit="Bps"),
        panel(901, "Queues backlog messages", "Maximum queue average from queueBacklogAdaptiveGroups in the latest complete bucket.", PROM,
              'max(cloudflare_queues_backlog_max_queue_avg_messages{service_name="cf2otel"})'),
        panel(902, "Queues backlog bytes", "Maximum queue average from queueBacklogAdaptiveGroups in the latest complete bucket.", PROM,
              'max(cloudflare_queues_backlog_max_queue_avg_bytes{service_name="cf2otel"})', unit="bytes"),
        panel(903, "Queues consumer concurrency", "Maximum queue average from queueConsumerMetricsAdaptiveGroups in the latest complete bucket.", PROM,
              'max(cloudflare_queues_consumer_max_queue_avg_concurrency{service_name="cf2otel"})'),
        panel(904, "Queues delayed backlog messages", "Maximum queue average from queueDelayedBacklogAdaptiveGroups in the latest complete bucket.", PROM,
              'max(cloudflare_queues_delayed_backlog_max_queue_avg_messages{service_name="cf2otel"})'),
        panel(905, "Queues message operations", "Message operation rate from queueMessageOperationsAdaptiveGroups.", PROM,
              'sum(rate(cloudflare_queues_message_operations_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
        panel(906, "Queues billable operations", "Billable operation rate from queueMessageOperationsAdaptiveGroups.", PROM,
              'sum(rate(cloudflare_queues_message_billable_operations_total{service_name="cf2otel"}[$__rate_interval]))', unit="reqps"),
    ])
    return {"apiVersion": "dashboard.grafana.app/v2", "kind": "Dashboard", "metadata": {"name": "cf2otel"},
        "spec": {"title": "Cloudflare to OpenTelemetry", "description": "Access, protected HTTP, AI Gateway, platform analytics and collector health. Generated by grafana/build_dashboard.py.",
        "tags": ["cloudflare", "cf2otel", "generated"], "annotations": [], "links": [], "preload": False, "cursorSync": "Crosshair",
        "timeSettings": {"from": "now-6h", "to": "now", "autoRefresh": "1m", "autoRefreshIntervals": ["1m", "5m", "15m", "1h"], "timezone": "browser", "hideTimepicker": False, "fiscalYearStartMonth": 0},
        "variables": [datasource("ds_prometheus", "prometheus", "grafanacloud-prom"), datasource("ds_loki", "loki", "grafanacloud-logs")],
        "elements": p, "layout": {"kind": "TabsLayout", "spec": {"tabs": [tab("Access", [101, 102, 103]), tab("Protected HTTP", [201, 202, 203]), tab("AI Gateway", [301, 302, 303, 304]), tab("Collector", [401, 402, 403, 404]), platform_tab()]}}}}


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    content = json.dumps(render(), indent=2, sort_keys=True) + "\n"
    if args.check:
        if not OUT.exists() or OUT.read_text(encoding="utf-8") != content:
            raise SystemExit("generated dashboard drift")
    else:
        OUT.parent.mkdir(parents=True, exist_ok=True)
        OUT.write_text(content, encoding="utf-8")


if __name__ == "__main__":
    main()
