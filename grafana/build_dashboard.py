#!/usr/bin/env python3
"""Generate the cf2otel Grafana Dashboard v2 resource.

The dashboard is a Grafana Dashboard v2 resource (``elements`` plus a ``TabsLayout``) consumed by
GitSync. Never hand-edit ``dashboards/cf2otel.json``; change this file and run
``python3 grafana/build_dashboard.py``. Alert rules in ``build_rules.py`` link to panels 401, 403,
404, 405 and 406 by id, so keep those ids stable.
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "dashboards" / "cf2otel.json"
PROM = "${ds_prometheus}"
LOKI = "${ds_loki}"
TEMPO = "${ds_tempo}"
VIZ_VERSION = "12.1.0"

S = 'service_name="cf2otel"'
# Entity filters. A variable's "All" value is ".*", which also matches series without the label.
HTTP = S + ',cloudflare_http_zone=~"$zone",cloudflare_http_host=~"$host"'
HTTP_ZONE = S + ',cloudflare_http_zone=~"$zone"'
CERT = S + ',cloudflare_certificate_zone=~"$zone"'


def certs_packs_interval_seconds() -> int:
    """Generator setting must match the deployment's certs.packs interval.

    The default is config.Default().Collectors["certs.packs"].Interval (1h).
    This is not a new exporter configuration key.
    """
    key = "GRAFANA_CERTS_PACKS_INTERVAL_SECONDS"
    value = os.environ.get(key, "3600")
    if not value.isascii() or not value.isdecimal():
        raise SystemExit(f"{key} must be a positive integer in seconds")
    try:
        interval = int(value)
    except ValueError:
        raise SystemExit(f"{key} must be a positive integer in seconds") from None
    if interval <= 0:
        raise SystemExit(f"{key} must be a positive integer in seconds")
    return interval


def access_seats_interval_seconds() -> int:
    """Deployment generator setting, not an observed runtime polling interval."""
    key = "GRAFANA_ACCESS_SEATS_INTERVAL_SECONDS"
    value = os.environ.get(key, "900")
    if not value.isascii() or not value.isdecimal():
        raise SystemExit(f"{key} must be a positive integer in seconds")
    try:
        interval = int(value)
    except ValueError:
        raise SystemExit(f"{key} must be a positive integer in seconds") from None
    if interval <= 0:
        raise SystemExit(f"{key} must be a positive integer in seconds")
    return interval


ACCESS_SEATS_INTERVAL_SECONDS = access_seats_interval_seconds()
ACCESS_SEATS_FRESHNESS_SECONDS = 3 * ACCESS_SEATS_INTERVAL_SECONDS
ACCESS_SEATS_FRESH = '(time() - max by (instance) (cf2otel_scrape_last_success_timestamp_seconds{service_name="cf2otel",cf2otel_collector="access.seats"}) < ' + str(ACCESS_SEATS_FRESHNESS_SECONDS) + ')'

CERTS_PACKS_INTERVAL_SECONDS = certs_packs_interval_seconds()
CERTS_PACKS_FRESHNESS_SECONDS = 3 * CERTS_PACKS_INTERVAL_SECONDS
# OTLP service.instance.id translates to the Prometheus instance label.
CERT_FRESH = '(time() - max by (instance) (cf2otel_scrape_last_success_timestamp_seconds{service_name="cf2otel",cf2otel_collector="certs.packs"}) < ' + str(CERTS_PACKS_FRESHNESS_SECONDS) + ')'
FW = S + ',cloudflare_firewall_zone=~"$zone"'
DNS = S + ',cloudflare_dns_zone=~"$zone"'
GW = S + ',cloudflare_ai_gateway_gateway_name=~"$gateway"'
RUM = S + ',cloudflare_rum_site_tag=~"$site_tag"'
WRK = S + ',cloudflare_workers_script_name=~"$script"'
R2 = S + ',cloudflare_r2_bucket_name=~"$bucket"'
LOG = '{service_name="cf2otel"}'

CACHED = "hit|stale|updating|revalidated"
CACHEABLE = CACHED + "|miss|expired"

GREEN, YELLOW, ORANGE, RED, BLUE, PURPLE = "green", "yellow", "orange", "red", "blue", "purple"
STATUS_COLORS = {"1xx": PURPLE, "2xx": GREEN, "3xx": BLUE, "4xx": ORANGE, "5xx": RED, "0xx": "text"}

# Tab titles double as URL slugs (dtab=<title with spaces as hyphens>).
TAB_HTTP = "HTTP and cache"
TAB_SECURITY = "Security"
TAB_DNS = "DNS"
TAB_ACCESS = "Access and Zero Trust"
TAB_AI = "AI Gateway"
TAB_WEB = "Web Analytics"
TAB_PLATFORM = "Workers and platform"
TAB_COLLECTOR = "Collector health"


# --------------------------------------------------------------------------------------------
# Queries
# --------------------------------------------------------------------------------------------

def prom(expr: str, legend: str = "", *, ref: str = "A", instant: bool = False, fmt: str = "time_series") -> dict:
    return _query(PROM, "prometheus", {"expr": expr, "refId": ref, "instant": instant, "range": not instant,
        "legendFormat": legend, "format": fmt, "editorMode": "code"}, ref)


def table_q(expr: str, ref: str = "A") -> dict:
    return prom(expr, ref=ref, instant=True, fmt="table")


def loki(expr: str, legend: str = "", *, ref: str = "A", instant: bool = False) -> dict:
    return _query(LOKI, "loki", {"expr": expr, "refId": ref, "queryType": "instant" if instant else "range",
        "legendFormat": legend, "editorMode": "code"}, ref)


def tempo(traceql: str, ref: str = "A", limit: int = 20) -> dict:
    return _query(TEMPO, "tempo", {"query": traceql, "refId": ref, "queryType": "traceql", "limit": limit,
        "tableType": "traces"}, ref)


def _query(ds: str, group: str, spec: dict, ref: str) -> dict:
    return {"kind": "PanelQuery", "spec": {"refId": ref, "hidden": False, "query": {"kind": "DataQuery",
        "version": "v0", "group": group, "datasource": {"name": ds}, "spec": spec}}}


def audit_annotations() -> list[dict]:
    """Loki's annotation API reads templates and expr from legacyOptions.

    Grafana's v2 adapter restores these at the annotation root; query.spec is
    empty because Loki annotationQuery does not consume the panel query target.
    Structured metadata is included in the returned log row labels.
    """
    actor = "Actor: {{cloudflare_audit_actor_email}} / {{cloudflare_audit_actor_token_name}} ({{cloudflare_audit_actor_type}})"
    action = "Action: {{cloudflare_audit_action_description}} ({{cloudflare_audit_action_type}})"
    resource = "Resource: {{cloudflare_audit_resource_product}} / {{cloudflare_audit_resource_type}} / {{cloudflare_audit_resource_id}}"
    query = _query(LOKI, "loki", {}, "A")["spec"]["query"]
    return [{"kind": "AnnotationQuery", "spec": {
        "name": "Account audit changes", "enable": False, "hide": False, "iconColor": BLUE,
        "query": query,
        "legacyOptions": {
            "expr": LOG + ' | event_name="cloudflare.audit.event"', "maxLines": 100, "instant": False,
            "titleFormat": actor + " | " + action,
            "textFormat": actor + " | " + action + " | " + resource,
            "tagKeys": "",
        },
    }}]


# --------------------------------------------------------------------------------------------
# Field config helpers
# --------------------------------------------------------------------------------------------

def steps(*pairs: tuple[str, float | None]) -> dict:
    return {"mode": "absolute", "steps": [{"color": c, "value": v} for c, v in pairs]}


NEUTRAL = steps((BLUE, None))
ZERO_GOOD = steps((GREEN, None), (RED, 1))
ZERO_GOOD_WARN = steps((GREEN, None), (ORANGE, 1))


def _props(props: dict) -> list[dict]:
    """Override properties. ``custom`` expands to one ``custom.<key>`` property per key; a whole-object
    ``custom`` override is silently ignored by Grafana."""
    out = []
    for key, value in props.items():
        if key == "custom":
            out.extend({"id": f"custom.{k}", "value": v} for k, v in value.items())
        else:
            out.append({"id": key, "value": value})
    return out


def by_name(name: str, **props) -> dict:
    return {"matcher": {"id": "byName", "options": name}, "properties": _props(props)}


def by_regexp(pattern: str, **props) -> dict:
    return {"matcher": {"id": "byRegexp", "options": pattern}, "properties": _props(props)}


def fixed(color: str) -> dict:
    return {"mode": "fixed", "fixedColor": color}


def color_overrides(colors: dict[str, str]) -> list[dict]:
    return [by_name(name, color=fixed(color)) for name, color in colors.items()]


def reduce(calc: str = "lastNotNull") -> dict:
    return {"calcs": [calc], "fields": "", "values": False}


# --------------------------------------------------------------------------------------------
# Panels
# --------------------------------------------------------------------------------------------

class Dashboard:
    def __init__(self) -> None:
        self.elements: dict[str, dict] = {}

    def panel(self, pid: int, title: str, description: str, viz: str, queries: list[dict], *, options: dict | None = None,
              defaults: dict | None = None, overrides: list[dict] | None = None, transformations: list[dict] | None = None,
              interval: str | None = None, links: list[dict] | None = None) -> int:
        key = f"panel-{pid}"
        if key in self.elements:
            raise ValueError(f"duplicate panel id {pid}")
        query_options = {"interval": interval} if interval else {}
        self.elements[key] = {"kind": "Panel", "spec": {"id": pid, "title": title, "description": description,
            "links": links or [], "data": {"kind": "QueryGroup", "spec": {"queries": queries,
            "queryOptions": query_options, "transformations": [_transformation(t) for t in transformations or []]}},
            "vizConfig": {"kind": "VizConfig", "group": viz, "version": VIZ_VERSION,
            "spec": {"options": options or {}, "fieldConfig": {"defaults": defaults or {}, "overrides": overrides or []}}}}}
        return pid

    # -- time series ------------------------------------------------------------------------
    def ts(self, pid: int, title: str, description: str, queries: list[dict], *, unit: str = "short", stack: bool = False,
           bars: bool = False, legend: str = "list", overrides: list[dict] | None = None, decimals: int | None = None,
           thresholds: dict | None = None, threshold_style: str = "off", interval: str | None = "5m",
           fill: int | None = None, min_zero: bool = True, transformations: list[dict] | None = None, no_value: str | None = None) -> int:
        custom = {"drawStyle": "bars" if bars else "line", "lineWidth": 1, "fillOpacity": fill if fill is not None else (80 if bars else (35 if stack else 12)),
            "gradientMode": "none" if bars else "opacity", "showPoints": "never", "spanNulls": False, "lineInterpolation": "linear",
            "stacking": {"mode": "normal" if stack else "none", "group": "A"}, "thresholdsStyle": {"mode": threshold_style},
            "axisPlacement": "auto", "axisSoftMin": 0 if min_zero else None, "barAlignment": 0}
        if not min_zero:
            custom.pop("axisSoftMin")
        defaults = {"unit": unit, "custom": custom, "color": {"mode": "palette-classic"}}
        if decimals is not None:
            defaults["decimals"] = decimals
        if thresholds:
            defaults["thresholds"] = thresholds
        if no_value:
            defaults["noValue"] = no_value
        legend_opts = {"showLegend": True, "displayMode": "list", "placement": "bottom", "calcs": []}
        if legend == "table":
            legend_opts = {"showLegend": True, "displayMode": "table", "placement": "right", "calcs": ["mean", "max"], "sortBy": "Max", "sortDesc": True}
        elif legend == "hidden":
            legend_opts = {"showLegend": False, "displayMode": "list", "placement": "bottom", "calcs": []}
        return self.panel(pid, title, description, "timeseries", queries,
            options={"legend": legend_opts, "tooltip": {"mode": "multi", "sort": "desc"}},
            defaults=defaults, overrides=overrides, interval=interval, transformations=transformations)

    # -- single values ----------------------------------------------------------------------
    def stat(self, pid: int, title: str, description: str, queries: list[dict], *, unit: str = "short",
             thresholds: dict | None = None, decimals: int | None = None, text_mode: str = "value", calc: str = "lastNotNull",
             color_mode: str | None = None, mappings: list[dict] | None = None, links: list[dict] | None = None,
             overrides: list[dict] | None = None, graph: bool = False, interval: str | None = None, no_value: str | None = None) -> int:
        defaults = {"unit": unit, "color": {"mode": "thresholds"}, "thresholds": thresholds or NEUTRAL, "mappings": mappings or []}
        if decimals is not None:
            defaults["decimals"] = decimals
        if no_value:
            defaults["noValue"] = no_value
        return self.panel(pid, title, description, "stat", queries,
            options={"reduceOptions": reduce(calc), "colorMode": color_mode or ("value" if thresholds else "none"),
                "graphMode": "area" if graph else "none", "textMode": text_mode, "justifyMode": "center",
                "orientation": "auto", "wideLayout": True, "showPercentChange": False},
            defaults=defaults, overrides=overrides, links=links, interval=interval)

    def gauge(self, pid: int, title: str, description: str, queries: list[dict], *, unit: str, thresholds: dict,
              min_value: float = 0, max_value: float = 1, decimals: int | None = None) -> int:
        defaults = {"unit": unit, "min": min_value, "max": max_value, "color": {"mode": "thresholds"}, "thresholds": thresholds}
        if decimals is not None:
            defaults["decimals"] = decimals
        return self.panel(pid, title, description, "gauge", queries,
            options={"reduceOptions": reduce(), "showThresholdLabels": False, "showThresholdMarkers": True, "sizing": "auto"},
            defaults=defaults)

    def bargauge(self, pid: int, title: str, description: str, queries: list[dict], *, unit: str = "short",
                 thresholds: dict | None = None, decimals: int | None = None, mode: str = "gradient", color: str = "continuous-BlPu",
                 transformations: list[dict] | None = None, all_values: bool = False, display_name: str | None = None) -> int:
        defaults = {"unit": unit, "min": 0, "color": {"mode": "thresholds"} if thresholds else {"mode": color},
            "thresholds": thresholds or NEUTRAL}
        if decimals is not None:
            defaults["decimals"] = decimals
        if display_name:
            defaults["displayName"] = display_name
        return self.panel(pid, title, description, "bargauge", queries,
            options={"reduceOptions": {**reduce(), "values": all_values}, "orientation": "horizontal", "displayMode": mode, "valueMode": "color",
                "namePlacement": "left", "showUnfilled": True, "sizing": "auto", "minVizHeight": 16, "minVizWidth": 8,
                "maxVizHeight": 24, "legend": {"showLegend": False, "displayMode": "list", "placement": "bottom", "calcs": []}},
            defaults=defaults, transformations=transformations)

    def donut(self, pid: int, title: str, description: str, queries: list[dict], *, unit: str = "short",
              overrides: list[dict] | None = None, decimals: int | None = None) -> int:
        defaults = {"unit": unit, "color": {"mode": "palette-classic"}}
        if decimals is not None:
            defaults["decimals"] = decimals
        return self.panel(pid, title, description, "piechart", queries,
            options={"pieType": "donut", "reduceOptions": reduce(), "displayLabels": ["percent"],
                "legend": {"showLegend": True, "displayMode": "table", "placement": "right", "values": ["value", "percent"]},
                "tooltip": {"mode": "single", "sort": "none"}},
            defaults=defaults, overrides=overrides)

    # -- tables -----------------------------------------------------------------------------
    def table(self, pid: int, title: str, description: str, queries: list[dict], *, columns: dict[str, str] | None = None,
              order: list[str] | None = None, hide: list[str] | None = None, overrides: list[dict] | None = None,
              sort_by: str | None = None, unit: str = "short", pre: list[dict] | None = None, merge: bool = True,
              decimals: int | None = None) -> int:
        """A table. ``columns`` renames source fields; ``order`` fixes column order by source name."""
        transformations = list(pre or [])
        if merge and len(queries) > 1:
            transformations.append({"id": "merge", "options": {}})
        exclude = {name: True for name in (hide or [])}
        exclude.setdefault("Time", True)
        organize: dict = {"excludeByName": exclude, "renameByName": columns or {}, "indexByName": {}}
        if order:
            organize["indexByName"] = {name: i for i, name in enumerate(order)}
        transformations.append({"id": "organize", "options": organize})
        options = {"showHeader": True, "cellHeight": "sm", "footer": {"show": False, "reducer": ["sum"], "countRows": False}}
        if sort_by:
            options["sortBy"] = [{"displayName": sort_by, "desc": True}]
        defaults = {"unit": unit, "custom": {"align": "auto", "cellOptions": {"type": "auto"}, "filterable": False}}
        if decimals is not None:
            defaults["decimals"] = decimals
        return self.panel(pid, title, description, "table", queries, options=options, defaults=defaults,
            overrides=overrides, transformations=transformations)

    # -- geomap -----------------------------------------------------------------------------
    def geomap(self, pid: int, title: str, description: str, queries: list[dict], *, country_field: str, value_field: str,
               transformations: list[dict] | None = None, unit: str = "short") -> int:
        layer = {"type": "markers", "name": "Countries", "tooltip": True,
            "location": {"mode": "lookup", "lookup": country_field, "gazetteer": "public/gazetteer/countries.json"},
            "config": {"showLegend": True, "style": {
                "size": {"field": value_field, "min": 4, "max": 30, "fixed": 5},
                "color": {"field": value_field},
                "opacity": 0.55, "rotation": {"fixed": 0, "min": -360, "max": 360, "mode": "mod"},
                "symbol": {"mode": "fixed", "fixed": "img/icons/marker/circle.svg"}, "symbolAlign": {"horizontal": "center", "vertical": "center"},
                "textConfig": {"fontSize": 12, "offsetX": 0, "offsetY": 0, "textAlign": "center", "textBaseline": "middle"}}}}
        return self.panel(pid, title, description, "geomap", queries,
            options={"view": {"id": "fit", "lat": 30, "lon": 10, "zoom": 1, "allLayers": True, "padding": 10, "maxZoom": 4},
                "controls": {"showZoom": True, "mouseWheelZoom": False, "showAttribution": True},
                "basemap": {"type": "default", "name": "Basemap", "config": {}}, "layers": [layer],
                "tooltip": {"mode": "details"}},
            defaults={"unit": unit, "color": {"mode": "continuous-YlRd"}, "thresholds": NEUTRAL},
            transformations=transformations)

    # -- heatmap ----------------------------------------------------------------------------
    def heatmap(self, pid: int, title: str, description: str, queries: list[dict], *, unit: str = "s", interval: str = "5m") -> int:
        return self.panel(pid, title, description, "heatmap", queries,
            options={"calculate": False, "cellGap": 1, "showValue": "never", "filterValues": {"le": 1e-9},
                "yAxis": {"axisPlacement": "left", "unit": unit, "reverse": False},
                "rowsFrame": {"layout": "auto"},
                "color": {"mode": "scheme", "scheme": "Oranges", "fill": "dark-orange", "scale": "exponential", "exponent": 0.5,
                    "steps": 64, "reverse": False},
                "tooltip": {"mode": "single", "yHistogram": True, "showColorScale": False},
                "legend": {"show": True}, "exemplars": {"color": "rgba(255,0,255,0.7)"}},
            defaults={"custom": {"hideFrom": {"legend": False, "tooltip": False, "viz": False}, "scaleDistribution": {"type": "linear"}}},
            interval=interval)

    def state_timeline(self, pid: int, title: str, description: str, queries: list[dict], *, unit: str, thresholds: dict,
                       mappings: list[dict] | None = None) -> int:
        return self.panel(pid, title, description, "state-timeline", queries,
            options={"mergeValues": True, "showValue": "never", "alignValue": "left", "rowHeight": 0.85,
                "legend": {"showLegend": True, "displayMode": "list", "placement": "bottom"},
                "tooltip": {"mode": "single", "sort": "none"}},
            defaults={"unit": unit, "color": {"mode": "thresholds"}, "thresholds": thresholds, "mappings": mappings or [],
                "custom": {"lineWidth": 0, "fillOpacity": 80}})

    def logs(self, pid: int, title: str, description: str, expr: str) -> int:
        return self.panel(pid, title, description, "logs", [loki(expr)],
            options={"showTime": True, "showLabels": False, "showCommonLabels": False, "wrapLogMessage": True,
                "prettifyLogMessage": False, "enableLogDetails": True, "enableInfiniteScrolling": True,
                "dedupStrategy": "none", "sortOrder": "Descending"})

    def text(self, pid: int, title: str, content: str) -> int:
        return self.panel(pid, title, "", "text", [], options={"mode": "markdown", "content": content,
            "code": {"language": "plaintext", "showLineNumbers": False, "showMiniMap": False}})


def _transformation(step: dict) -> dict:
    """Dashboard v2 wraps each transformation as {kind, spec}; a bare {id, options} is silently ignored."""
    return {"kind": step["id"], "spec": {"id": step["id"], "options": step.get("options", {})}}


def tab_link(tab: str, title: str | None = None) -> dict:
    slug = tab.replace(" ", "-")
    return {"title": title or f"Open the {tab} tab", "url": f"?dtab={slug}&${{__url_time_range}}&${{__all_variables}}", "targetBlank": False}


def loki_rows(value_name: str) -> list[dict]:
    """Name the value column of a Loki instant metric query.

    Grafana's Loki data source already turns an instant vector into one table frame (label columns plus a
    value column), so this only renames the value column and sorts by it.
    """
    return [{"id": "organize", "options": {"excludeByName": {"Time": True}, "renameByName": {"Value #A": value_name, "Value": value_name},
                                           "indexByName": {}}},
            {"id": "sortBy", "options": {"sort": [{"field": value_name, "desc": True}]}}]


def mapping_range(start: float, end: float, text: str, color: str) -> dict:
    return {"type": "range", "options": {"from": start, "to": end, "result": {"text": text, "color": color, "index": 0}}}


# --------------------------------------------------------------------------------------------
# Layout
# --------------------------------------------------------------------------------------------

def grid(items: list[tuple[int, int, int]]) -> dict:
    """Pack (panel id, width, height) left to right, wrapping at 24 columns."""
    out, x, y, row_h = [], 0, 0, 0
    for pid, w, h in items:
        if x + w > 24:
            x, y, row_h = 0, y + row_h, 0
        out.append({"kind": "GridLayoutItem", "spec": {"x": x, "y": y, "width": w, "height": h,
            "element": {"kind": "ElementReference", "name": f"panel-{pid}"}}})
        x += w
        row_h = max(row_h, h)
    return {"kind": "GridLayout", "spec": {"items": out}}


def row(title: str, items: list[tuple[int, int, int]], *, collapse: bool = False, hide_header: bool = False) -> dict:
    spec = {"title": title, "collapse": collapse, "layout": grid(items)}
    if hide_header:
        spec["hideHeader"] = True
    return {"kind": "RowsLayoutRow", "spec": spec}


def tab(title: str, rows: list[dict]) -> dict:
    return {"kind": "TabsLayoutTab", "spec": {"title": title, "layout": {"kind": "RowsLayout", "spec": {"rows": rows}}}}


# --------------------------------------------------------------------------------------------
# Tabs
# --------------------------------------------------------------------------------------------

def overview(d: Dashboard) -> dict:
    nav = " · ".join(f"[{t}](?dtab={t.replace(' ', '-')}&${{__url_time_range}}&${{__all_variables}})" for t in
        (TAB_HTTP, TAB_SECURITY, TAB_DNS, TAB_ACCESS, TAB_AI, TAB_WEB, TAB_PLATFORM, TAB_COLLECTOR))
    d.text(1000, "", "**cf2otel** polls Cloudflare's REST and GraphQL APIs and exports OTLP. Headline numbers are totals over the "
        "selected time range; the zone and host filters apply to HTTP, security and DNS panels. Deep dives: " + nav)

    # cf2otel health.
    d.stat(1001, "Collectors reporting", "Distinct collectors that exported a last-success timestamp in the latest sample. "
        "Compare with the collectors you enabled; a missing collector never registered.",
        [prom(f'count(max by (cf2otel_collector) (cf2otel_scrape_last_success_timestamp_seconds{{{S}}}))', instant=True)],
        links=[tab_link(TAB_COLLECTOR)])
    d.stat(1002, "Stale collectors", "Collectors whose last successful poll is older than 15 minutes (or that never succeeded). "
        "Matches the cf2otel collector-stale alert threshold. Zero is healthy.",
        [prom(f'count((time() - max by (cf2otel_collector) (cf2otel_scrape_last_success_timestamp_seconds{{{S}}})) > 900) or vector(0)', instant=True)],
        thresholds=ZERO_GOOD, links=[tab_link(TAB_COLLECTOR)])
    d.stat(1003, "Collector errors", "Failed collector polls over the selected range, all collectors. A failed poll retries the same window, "
        "so errors alone are not data loss.",
        [prom(f'sum(increase(cf2otel_scrape_errors_total{{{S}}}[$__range])) or vector(0)', instant=True)],
        thresholds=ZERO_GOOD_WARN, decimals=0, links=[tab_link(TAB_COLLECTOR)])
    d.stat(1004, "Export failures", "Failed OTLP export batches over the selected range. Zero is healthy.",
        [prom(f'sum(increase(cf2otel_export_errors_total{{{S}}}[$__range])) or vector(0)', instant=True)],
        thresholds=ZERO_GOOD, decimals=0, links=[tab_link(TAB_COLLECTOR)])
    d.stat(1005, "Data permanently lost", "Retention-gap seconds skipped over the selected range, summed across collectors. "
        "Any nonzero value is source data that aged out before cf2otel could read it and can never be backfilled.",
        [prom(f'sum(increase(cf2otel_window_gap_seconds_total{{{S}}}[$__range])) or vector(0)', instant=True)],
        unit="s", thresholds=ZERO_GOOD, links=[tab_link(TAB_COLLECTOR)])
    d.stat(1006, "Running version", "cf2otel build version reported by each running instance.",
        [prom(f'count by (cf2otel_build_version) (cf2otel_build_info_ratio{{{S}}})', "{{cf2otel_build_version}}", instant=True)],
        text_mode="name")

    # Headline KPIs.
    link_http, link_sec, link_dns = [tab_link(TAB_HTTP)], [tab_link(TAB_SECURITY)], [tab_link(TAB_DNS)]
    d.stat(1011, "HTTP requests", "Edge requests over the selected range from sample-corrected httpRequestsAdaptiveGroups, for the selected zones and hosts.",
        [prom(f'sum(increase(cloudflare_http_requests_total{{{HTTP}}}[$__range]))', instant=True)], decimals=0, links=link_http)
    d.stat(1012, "Cache hit ratio", f"Share of cacheable requests served from cache: cache status {CACHED} over {CACHEABLE}. "
        "Dynamic, bypass and uncacheable (none) requests are excluded.",
        [prom(f'sum(increase(cloudflare_http_requests_total{{{HTTP},cloudflare_http_cache_status=~"{CACHED}"}}[$__range])) / '
              f'sum(increase(cloudflare_http_requests_total{{{HTTP},cloudflare_http_cache_status=~"{CACHEABLE}"}}[$__range]))', instant=True)],
        unit="percentunit", decimals=1, links=link_http)
    d.stat(1013, "5xx error ratio", "Share of edge requests answered with a 5xx status over the selected range. Above 1% is worth a look, above 5% is an incident.",
        [prom(f'(sum(increase(cloudflare_http_requests_total{{{HTTP},cf2otel_status_class="5xx"}}[$__range])) or vector(0)) / '
              f'sum(increase(cloudflare_http_requests_total{{{HTTP}}}[$__range]))', instant=True)],
        unit="percentunit", decimals=2, thresholds=steps((GREEN, None), (ORANGE, 0.01), (RED, 0.05)), links=link_http)
    d.stat(1014, "Security events", "Firewall and WAF events over the selected range from a firewall Groups dataset, for the selected zones.",
        [prom(f'sum(increase(cloudflare_firewall_events_total{{{FW}}}[$__range]))', instant=True)], decimals=0, links=link_sec)
    d.stat(1015, "Authoritative DNS queries", "Queries answered by Cloudflare authoritative DNS for the selected zones, from dnsAnalyticsAdaptiveGroups.",
        [prom(f'sum(increase(cloudflare_dns_queries_total{{{DNS}}}[$__range]))', instant=True)], decimals=0, links=link_dns)
    d.stat(1016, "Gateway DNS queries", "Account-level Zero Trust Gateway resolver queries over the selected range.",
        [prom(f'sum(increase(cloudflare_gateway_dns_queries_total{{{S}}}[$__range]))', instant=True)], decimals=0, links=link_dns)
    d.stat(1017, "Human Access logins", "Exact Access login events from the REST log over the selected range; service-token traffic is excluded by source. "
        "Counted from logs because a counter series first seen inside the range is undercounted by increase().",
        [loki(f'sum(count_over_time({LOG} | event_name="cloudflare.access.login" [$__range]))', instant=True)],
        decimals=0, links=[tab_link(TAB_ACCESS)], no_value="0")
    d.stat(1018, "AI Gateway requests", "Completed AI Gateway requests from the REST gateway log over the selected range, for the selected gateways.",
        [prom(f'sum(increase(cloudflare_ai_gateway_requests_total{{{GW}}}[$__range]))', instant=True)], decimals=0, links=[tab_link(TAB_AI)])
    d.stat(1019, "AI Gateway cost", "Gateway-reported cost over the selected range. The currency is not stated by the API, so no currency unit is asserted.",
        [prom(f'sum(increase(cloudflare_ai_gateway_cost_total{{{GW}}}[$__range]))', instant=True)], unit="none", decimals=2, links=[tab_link(TAB_AI)])
    d.stat(1020, "AI tokens", "Input plus output tokens over the selected range. Cached and reasoning tokens are subsets and are not added again.",
        [prom(f'sum(increase(gen_ai_client_inference_usage_input_tokens_total{{{GW}}}[$__range])) + '
              f'sum(increase(gen_ai_client_inference_usage_output_tokens_total{{{GW}}}[$__range]))', instant=True)],
        decimals=0, links=[tab_link(TAB_AI)])
    d.stat(1021, "Web page views", "Browser page loads measured by Web Analytics (RUM) over the selected range, for the selected sites.",
        [prom(f'sum(increase(cloudflare_rum_page_views_total{{{RUM}}}[$__range]))', instant=True)], decimals=0, links=[tab_link(TAB_WEB)])
    d.stat(1022, "Workers requests", "Workers invocations over the selected range, for the selected scripts.",
        [prom(f'sum(increase(cloudflare_workers_requests_total{{{WRK}}}[$__range]))', instant=True)], decimals=0, links=[tab_link(TAB_PLATFORM)])

    d.ts(1030, "Edge requests by status class", "Sample-corrected edge request rate by response status class for the selected zones and hosts.",
        [prom(f'sum by (cf2otel_status_class) (rate(cloudflare_http_requests_total{{{HTTP}}}[$__rate_interval]))', "{{cf2otel_status_class}}")],
        unit="reqps", stack=True, overrides=color_overrides(STATUS_COLORS))
    d.geomap(1031, "Security events by client country", "Where firewall and WAF events came from. Counted from the per-event firewall log, "
        "which is Cloudflare's adaptive sample, so read it as a distribution rather than a total.",
        [loki(f'sum by (cloudflare_firewall_client_country) (count_over_time({LOG} | event_name="cloudflare.firewall.event" '
              f'| cloudflare_firewall_zone=~"$zone" [$__range]))', instant=True)],
        country_field="cloudflare_firewall_client_country", value_field="Sampled events", transformations=loki_rows("Sampled events"))
    d.ts(1032, "Cloudflare API calls by status class", "cf2otel's own Cloudflare API request rate. 4xx and 5xx here mean polls are failing; "
        "0xx means the request never got an HTTP response (network error or timeout).",
        [prom(f'sum by (cf2otel_status_class) (rate(cf2otel_api_requests_total{{{S}}}[$__rate_interval]))', "{{cf2otel_status_class}}")],
        unit="reqps", stack=True, overrides=color_overrides(STATUS_COLORS))

    return tab("Overview", [
        row("About", [(1000, 24, 2)], hide_header=True),
        row("cf2otel health", [(1001, 4, 4), (1002, 4, 4), (1003, 4, 4), (1004, 4, 4), (1005, 4, 4), (1006, 4, 4)]),
        row("Cloudflare estate", [(1011, 4, 4), (1012, 4, 4), (1013, 4, 4), (1014, 4, 4), (1015, 4, 4), (1016, 4, 4),
                                  (1017, 4, 4), (1018, 4, 4), (1019, 4, 4), (1020, 4, 4), (1021, 4, 4), (1022, 4, 4),
                                  (1030, 12, 10), (1031, 12, 10)]),
        row("Poller", [(1032, 24, 6)]),
    ])


def http_tab(d: Dashboard) -> dict:
    total = f'sum(increase(cloudflare_http_requests_total{{{HTTP}}}[$__range]))'
    d.stat(2001, "Requests", "Eyeball requests by default (http.request_source can select all traffic), sample-corrected from httpRequestsAdaptiveGroups.",
        [prom(total, instant=True)], decimals=0)
    d.stat(2002, "Served from cache", f"Share of all requests with cache status {CACHED}. Lower than the cacheable hit ratio because dynamic and uncacheable traffic counts here.",
        [prom(f'sum(increase(cloudflare_http_requests_total{{{HTTP},cloudflare_http_cache_status=~"{CACHED}"}}[$__range])) / {total}', instant=True)],
        unit="percentunit", decimals=1)
    d.stat(2003, "Cacheable hit ratio", f"Cache status {CACHED} over {CACHEABLE}: how often a cacheable object was already in cache.",
        [prom(f'sum(increase(cloudflare_http_requests_total{{{HTTP},cloudflare_http_cache_status=~"{CACHED}"}}[$__range])) / '
              f'sum(increase(cloudflare_http_requests_total{{{HTTP},cloudflare_http_cache_status=~"{CACHEABLE}"}}[$__range]))', instant=True)],
        unit="percentunit", decimals=1)
    d.stat(2004, "4xx ratio", "Share of requests answered with a 4xx status. Scanners and blocked bots raise this without anything being broken.",
        [prom(f'(sum(increase(cloudflare_http_requests_total{{{HTTP},cf2otel_status_class="4xx"}}[$__range])) or vector(0)) / {total}', instant=True)],
        unit="percentunit", decimals=1)
    d.stat(2005, "5xx ratio", "Share of requests answered with a 5xx status. Above 1% is worth a look, above 5% is an incident.",
        [prom(f'(sum(increase(cloudflare_http_requests_total{{{HTTP},cf2otel_status_class="5xx"}}[$__range])) or vector(0)) / {total}', instant=True)],
        unit="percentunit", decimals=2, thresholds=steps((GREEN, None), (ORANGE, 0.01), (RED, 0.05)))
    d.stat(2006, "Hosts with traffic", "Distinct hostnames with at least one request in the selected range.",
        [prom(f'count(sum by (cloudflare_http_host) (increase(cloudflare_http_requests_total{{{HTTP}}}[$__range])) > 0)', instant=True)])

    d.ts(201, "Requests by status class", "Corrected request rate from httpRequestsAdaptiveGroups, not sampled raw events, stacked by edge status class.",
        [prom(f'sum by (cf2otel_status_class) (rate(cloudflare_http_requests_total{{{HTTP}}}[$__rate_interval]))', "{{cf2otel_status_class}}")],
        unit="reqps", stack=True, overrides=color_overrides(STATUS_COLORS))
    d.ts(2011, "Requests by cache status", "Corrected request rate stacked by Cloudflare cache status.",
        [prom(f'sum by (cloudflare_http_cache_status) (rate(cloudflare_http_requests_total{{{HTTP}}}[$__rate_interval]))', "{{cloudflare_http_cache_status}}")],
        unit="reqps", stack=True, overrides=color_overrides({"hit": GREEN, "miss": ORANGE, "expired": YELLOW, "dynamic": BLUE, "none": "text", "bypass": PURPLE}))
    d.donut(2012, "Cache status share", "Share of requests by cache status over the selected range.",
        [prom(f'sum by (cloudflare_http_cache_status) (increase(cloudflare_http_requests_total{{{HTTP}}}[$__range]))', "{{cloudflare_http_cache_status}}", instant=True)],
        overrides=color_overrides({"hit": GREEN, "miss": ORANGE, "expired": YELLOW, "dynamic": BLUE, "none": "text", "bypass": PURPLE}), decimals=0)
    d.bargauge(2013, "Top zones by requests", "The 15 busiest zones over the selected range.",
        [prom(f'sort_desc(topk(15, sum by (cloudflare_http_zone) (increase(cloudflare_http_requests_total{{{HTTP}}}[$__range]))))', "{{cloudflare_http_zone}}", instant=True)],
        decimals=0)
    active = f'(sum by (cloudflare_http_host) (increase(cloudflare_http_requests_total{{{HTTP}}}[$__range])) > 0)'
    d.table(2014, "Hosts", "Every host with traffic in the selected range: requests, error and cache ratios, and the mean origin response time. "
        "Hosts have high cardinality, so this is a scrollable table rather than a chart.",
        [table_q(active, "A"),
         table_q(f'(sum by (cloudflare_http_host) (increase(cloudflare_http_requests_total{{{HTTP},cf2otel_status_class="5xx"}}[$__range])) '
                 f'or 0 * {active}) / {active}', "B"),
         table_q(f'(sum by (cloudflare_http_host) (increase(cloudflare_http_requests_total{{{HTTP},cloudflare_http_cache_status=~"{CACHED}"}}[$__range])) '
                 f'or 0 * {active}) / {active}', "C"),
         table_q(f'avg by (cloudflare_http_host) (avg_over_time(cloudflare_http_origin_duration_seconds{{{HTTP}}}[$__range])) '
                 f'and on (cloudflare_http_host) {active}', "D")],
        columns={"cloudflare_http_host": "Host", "Value #A": "Requests", "Value #B": "5xx ratio", "Value #C": "Served from cache", "Value #D": "Origin time (mean)"},
        order=["cloudflare_http_host", "Value #A", "Value #B", "Value #C", "Value #D"], sort_by="Requests",
        overrides=[by_name("Requests", decimals=0),
                   by_name("5xx ratio", unit="percentunit", decimals=1, thresholds=steps((GREEN, None), (ORANGE, 0.01), (RED, 0.05)),
                           custom={"cellOptions": {"type": "color-text"}, "align": "auto"}),
                   by_name("Served from cache", unit="percentunit", decimals=0, max=1, min=0,
                           custom={"cellOptions": {"type": "gauge", "mode": "basic", "valueDisplayMode": "text"}, "align": "auto"}, color={"mode": "continuous-BlPu"}),
                   by_name("Origin time (mean)", unit="s", decimals=2)])

    d.ts(204, "Origin response time by host", "Average origin response duration per Groups window, in seconds, for the 10 slowest hosts at each point. "
        "This is a per-window mean from the Groups dataset, not a percentile.",
        [prom(f'topk(10, avg by (cloudflare_http_host) (cloudflare_http_origin_duration_seconds{{{HTTP}}}))', "{{cloudflare_http_host}}")],
        unit="s", legend="table", interval="5m")
    d.bargauge(2021, "Slowest origins", "Mean origin response time over the selected range, slowest 10 hosts.",
        [prom(f'sort_desc(topk(10, avg by (cloudflare_http_host) (avg_over_time(cloudflare_http_origin_duration_seconds{{{HTTP}}}[$__range]))))',
              "{{cloudflare_http_host}}", instant=True)],
        unit="s", decimals=2, thresholds=steps((GREEN, None), (YELLOW, 0.5), (RED, 2)), mode="lcd")

    d.logs(203, "Protected-host HTTP events", "Sampled per-request events. User identity, when present, is explicitly inferred.",
        f'{LOG} | event_name="cloudflare.http.request" | cloudflare_http_zone=~"$zone" | cloudflare_http_host=~"$host"')

    d.ts(2050, "Response bandwidth", "Sample-corrected response bytes on the request dimensions; eyeball traffic by default.",
        [prom(f'sum(rate(cloudflare_http_response_bytes_total{{{HTTP}}}[$__rate_interval]))', "response bytes")], unit="Bps")
    breakdowns = [
        (2051, "Edge status", "cloudflare_http_requests_by_status_total", "cloudflare_http_status_code"),
        (2052, "Origin status", "cloudflare_http_requests_by_status_total", "cloudflare_http_origin_status_code"),
        (2053, "HTTP protocol", "cloudflare_http_requests_by_protocol_total", "cloudflare_http_protocol"),
        (2054, "TLS protocol", "cloudflare_http_requests_by_protocol_total", "cloudflare_http_tls_protocol"),
        (2055, "Method", "cloudflare_http_requests_by_method_total", "cloudflare_http_method"),
        (2056, "Content type", "cloudflare_http_requests_by_content_type_total", "cloudflare_http_content_type"),
    ]
    for pid, title, metric, label in breakdowns:
        d.ts(pid, title, "Zone-level Groups request rate, not filtered by host. Empty when this breakdown is disabled or unavailable.",
            [prom(f'sum by ({label}) (rate({metric}{{{HTTP_ZONE},{label}!=""}}[$__rate_interval]))', '{{' + label + '}}')], unit="reqps", stack=True)
    d.geomap(2057, "HTTP requests by country", "Sample-corrected Groups requests by ISO country, zone-level; not filtered by host.",
        [table_q(f'sum by (cloudflare_http_client_country) (increase(cloudflare_http_requests_by_country_total{{{HTTP_ZONE},cloudflare_http_client_country!=""}}[$__range]))')],
        country_field="cloudflare_http_client_country", value_field="Value")
    d.ts(2058, "Response bandwidth by country", "Zone-level response byte rates by client country; not filtered by host.",
        [prom(f'sum by (cloudflare_http_client_country) (rate(cloudflare_http_response_bytes_by_country_total{{{HTTP_ZONE}}}[$__rate_interval]))', "{{cloudflare_http_client_country}}")], unit="Bps")
    for pid, title, metric, note in ((2059, "Edge TTFB statistics", "cloudflare_http_edge_ttfb_seconds", "Pro-only: no series on Free zones is expected."),
                                   (2060, "Origin response percentiles", "cloudflare_http_origin_response_time_seconds", "Available on Free and Pro zones.")):
        d.ts(pid, title, "Per-window statistics by zone and host, in seconds; not fleet-wide percentiles. " + note,
            [prom(f'{metric}{{{HTTP}}}', "{{cloudflare_http_zone}} / {{cloudflare_http_host}} {{cloudflare_statistic}}")], unit="s", legend="table")

    d.stat(2061, "Visits", "Zone-only adaptive Groups visits under the configured request-source policy; not filtered by host.",
        [prom(f'sum(increase(cloudflare_http_visits_total{{{HTTP_ZONE}}}[$__range]))', instant=True)], decimals=0)
    d.stat(2062, "Threats", "Zone-only complete UTC-hour threat rollups, held back at least ten minutes; not host-filtered or assumed eyeball-only.",
        [prom(f'sum(increase(cloudflare_http_threats_total{{{HTTP_ZONE}}}[$__range]))', instant=True)], decimals=0)
    d.stat(2063, "Account transfer month to date", "Account-wide eyeball response bytes from UTC month start through the latest held-back complete period. Not filtered by zone or host; refreshes hourly by default.",
        [prom(f'max(cloudflare_http_account_transfer_month_to_date_bytes{{{S}}})', instant=True)], unit="bytes")
    d.stat(2064, "Account projected monthly transfer", "Account-wide MTD eyeball bytes projected using exact UTC month duration and elapsed complete-period seconds. Not filtered by zone or host; an estimate, not a billing forecast.",
        [prom(f'max(cloudflare_http_account_transfer_projected_month_total_bytes{{{S}}})', instant=True)], unit="bytes")

    return tab(TAB_HTTP, [
        row("Summary", [(2001, 4, 4), (2002, 4, 4), (2003, 4, 4), (2004, 4, 4), (2005, 4, 4), (2006, 4, 4)]),
        row("Visits, threats and account transfer", [(2061, 6, 4), (2062, 6, 4), (2063, 6, 4), (2064, 6, 4)]),
        row("Traffic and cache", [(201, 12, 8), (2011, 12, 8), (2012, 8, 9), (2013, 16, 9)]),
        row("Bytes and country", [(2050, 12, 8), (2058, 12, 8), (2057, 24, 10)]),
        row("Zone breakdowns (not host-filtered)", [(pid, 12, 8) for pid, *_ in breakdowns]),
        row("Latency tails", [(2059, 12, 8), (2060, 12, 8)]),
        row("Hosts", [(2014, 24, 10)]),
        row("Origin", [(204, 16, 8), (2021, 8, 8)]),
        row("HTTP request logs", [(203, 24, 12)], collapse=True),
    ])


def security_tab(d: Dashboard) -> dict:
    fw_total = f'sum(increase(cloudflare_firewall_events_total{{{FW}}}[$__range]))'
    d.stat(2101, "Security events", "Firewall and WAF events over the selected range from a firewall Groups dataset.",
        [prom(fw_total, instant=True)], decimals=0)
    d.stat(2102, "Blocked", "Events whose action was block, as a share of all events. Free-plan zones report no action, so they count only in the total.",
        [prom(f'(sum(increase(cloudflare_firewall_events_total{{{FW},cloudflare_firewall_action="block"}}[$__range])) or vector(0)) / {fw_total}', instant=True)],
        unit="percentunit", decimals=1)
    d.stat(2103, "Challenged", "Events answered with a managed, JavaScript or interactive challenge over the selected range.",
        [prom(f'sum(increase(cloudflare_firewall_events_total{{{FW},cloudflare_firewall_action=~".*challenge.*"}}[$__range])) or vector(0)', instant=True)], decimals=0)
    d.stat(2104, "Logged or skipped", "Events that matched a rule in log or skip mode: rules that are watching, not acting.",
        [prom(f'sum(increase(cloudflare_firewall_events_total{{{FW},cloudflare_firewall_action=~"log|skip"}}[$__range])) or vector(0)', instant=True)], decimals=0)
    d.stat(2105, "Audit events", "Account audit log entries over the selected range.",
        [prom(f'sum(increase(cloudflare_audit_events_total{{{S}}}[$__range]))', instant=True)], decimals=0)
    d.stat(2106, "Failed audit actions", "Audit entries whose action result was not success. Zero is normal.",
        [prom(f'sum(increase(cloudflare_audit_events_total{{{S},cloudflare_audit_action_result!="success"}}[$__range])) or vector(0)', instant=True)],
        decimals=0, thresholds=ZERO_GOOD_WARN)

    unclassified = ('label_replace(sum by (cloudflare_firewall_action) (rate(cloudflare_firewall_events_total{%s}[$__rate_interval])), '
                    '"cloudflare_firewall_action", "unclassified (plan has no action field)", "cloudflare_firewall_action", "")') % FW
    d.ts(910, "Firewall events by action", "Security event rate from a Groups dataset by available action; Free plans without action/source dimensions "
        "still emit the zone total, shown as unclassified.", [prom(unclassified, "{{cloudflare_firewall_action}}")],
        unit="reqps", stack=True, overrides=color_overrides({"block": RED, "log": BLUE, "skip": "text", "managed_challenge": ORANGE, "jschallenge": YELLOW}))
    d.donut(2111, "Events by rule engine", "Which Cloudflare security product produced the events over the selected range (managed rules, custom rules, "
        "Browser Integrity Check and so on).",
        [prom(f'label_replace(sum by (cloudflare_firewall_source) (increase(cloudflare_firewall_events_total{{{FW}}}[$__range])), '
              f'"cloudflare_firewall_source", "unclassified", "cloudflare_firewall_source", "")', "{{cloudflare_firewall_source}}", instant=True)],
        decimals=0)
    d.bargauge(2112, "Events by zone", "Security events per zone over the selected range.",
        [prom(f'sort_desc(sum by (cloudflare_firewall_zone) (increase(cloudflare_firewall_events_total{{{FW}}}[$__range])))', "{{cloudflare_firewall_zone}}", instant=True)],
        decimals=0, color="continuous-YlRd")
    d.geomap(2113, "Client countries", "Where security events came from, counted from the per-event firewall log. The log is Cloudflare's adaptive sample, "
        "so compare countries with each other rather than with the totals above.",
        [loki(f'sum by (cloudflare_firewall_client_country) (count_over_time({LOG} | event_name="cloudflare.firewall.event" '
              f'| cloudflare_firewall_zone=~"$zone" [$__range]))', instant=True)],
        country_field="cloudflare_firewall_client_country", value_field="Sampled events", transformations=loki_rows("Sampled events"))
    d.table(2114, "Top matched rules", "Rules with the most sampled events in the selected range. Counts come from the sampled firewall log.",
        [loki(f'topk(15, sum by (cloudflare_firewall_rule_id, cloudflare_firewall_source, cloudflare_firewall_action) '
              f'(count_over_time({LOG} | event_name="cloudflare.firewall.event" | cloudflare_firewall_zone=~"$zone" [$__range])))', instant=True)],
        pre=loki_rows("Sampled events"),
        columns={"cloudflare_firewall_rule_id": "Rule ID", "cloudflare_firewall_source": "Engine", "cloudflare_firewall_action": "Action"},
        order=["cloudflare_firewall_rule_id", "cloudflare_firewall_source", "cloudflare_firewall_action", "Sampled events"], sort_by="Sampled events")
    d.table(2115, "Top targeted paths", "Host and path pairs with the most sampled security events in the selected range.",
        [loki(f'topk(15, sum by (cloudflare_firewall_host, cloudflare_firewall_path) '
              f'(count_over_time({LOG} | event_name="cloudflare.firewall.event" | cloudflare_firewall_zone=~"$zone" [$__range])))', instant=True)],
        pre=loki_rows("Sampled events"),
        columns={"cloudflare_firewall_host": "Host", "cloudflare_firewall_path": "Path"},
        order=["cloudflare_firewall_host", "cloudflare_firewall_path", "Sampled events"], sort_by="Sampled events")

    d.ts(913, "Audit events by product", "Audit events per bar interval by resource product; entries without a product are account-level and show as account.",
        [prom(f'label_replace(sum by (cloudflare_audit_resource_product) (increase(cloudflare_audit_events_total{{{S}}}[$__interval])), '
              f'"cloudflare_audit_resource_product", "account", "cloudflare_audit_resource_product", "")', "{{cloudflare_audit_resource_product}}")],
        unit="short", bars=True, stack=True, decimals=0, interval="15m")
    d.donut(2121, "Audit actions by type", "Create, update and delete actions recorded in the account audit log over the selected range.",
        [prom(f'sum by (cloudflare_audit_action_type) (increase(cloudflare_audit_events_total{{{S}}}[$__range]))', "{{cloudflare_audit_action_type}}", instant=True)],
        decimals=0, overrides=color_overrides({"create": GREEN, "update": BLUE, "delete": RED}))
    d.table(2122, "Recent configuration changes", "The latest account audit log entries: who changed what. Actor email and IP live only on the log, never on metrics.",
        [loki(f'{LOG} | event_name="cloudflare.audit.event"')],
        pre=[{"id": "extractFields", "options": {"source": "labels", "format": "auto", "replace": False, "keepTime": False}},
             {"id": "limit", "options": {"limitField": 50}}],
        merge=False,
        columns={"Time": "Time", "cloudflare_audit_actor_email": "Actor", "cloudflare_audit_actor_token_name": "API token",
                 "cloudflare_audit_action_description": "Action", "cloudflare_audit_resource_product": "Product",
                 "cloudflare_audit_resource_type": "Resource type", "cloudflare_audit_action_result": "Result"},
        order=["Time", "cloudflare_audit_actor_email", "cloudflare_audit_actor_token_name", "cloudflare_audit_action_description",
               "cloudflare_audit_resource_product", "cloudflare_audit_resource_type", "cloudflare_audit_action_result"])
    _include_only(d, 2122, ["Time", "cloudflare_audit_actor_email", "cloudflare_audit_actor_token_name", "cloudflare_audit_action_description",
                            "cloudflare_audit_resource_product", "cloudflare_audit_resource_type", "cloudflare_audit_action_result"])

    d.logs(2131, "Firewall events", "Per-request security actions from firewallEventsAdaptive; IP, path, query, user agent and ray stay on logs.",
        f'{LOG} | event_name="cloudflare.firewall.event" | cloudflare_firewall_zone=~"$zone"')
    d.logs(2132, "Audit events", "Account audit log v2 entries with actor, action, resource and request details.",
        f'{LOG} | event_name="cloudflare.audit.event"')

    return tab(TAB_SECURITY, [
        row("Summary", [(2101, 4, 4), (2102, 4, 4), (2103, 4, 4), (2104, 4, 4), (2105, 4, 4), (2106, 4, 4)]),
        row("Firewall and WAF", [(910, 16, 9), (2111, 8, 9), (2113, 12, 10), (2112, 12, 10), (2114, 12, 9), (2115, 12, 9)]),
        row("Account audit log", [(913, 12, 8), (2121, 12, 8), (2122, 24, 10)]),
        row("Firewall event logs", [(2131, 24, 12)], collapse=True),
        row("Audit logs", [(2132, 24, 12)], collapse=True),
    ])


def _include_only(d: Dashboard, pid: int, names: list[str]) -> None:
    """Switch a table's organize step from exclude-list to include-list (for log-derived columns)."""
    for step in d.elements[f"panel-{pid}"]["spec"]["data"]["spec"]["transformations"]:
        if step["kind"] == "organize":
            step["spec"]["options"]["excludeByName"] = {}
            step["spec"]["options"]["includeByName"] = {name: True for name in names}


def dns_tab(d: Dashboard) -> dict:
    dns_total = f'sum(increase(cloudflare_dns_queries_total{{{DNS}}}[$__range]))'
    d.stat(2201, "Queries", "Authoritative DNS queries for the selected zones over the selected range.", [prom(dns_total, instant=True)], decimals=0)
    d.stat(2202, "NXDOMAIN share", "Queries for names that do not exist. A steady share is normal; a jump often means scanning or a broken client.",
        [prom(f'(sum(increase(cloudflare_dns_queries_total{{{DNS},cloudflare_dns_response_code="NXDOMAIN"}}[$__range])) or vector(0)) / {dns_total}', instant=True)],
        unit="percentunit", decimals=1)
    d.stat(2203, "Refused or failed", "Queries answered REFUSED or SERVFAIL, as a share of all queries.",
        [prom(f'(sum(increase(cloudflare_dns_queries_total{{{DNS},cloudflare_dns_response_code=~"REFUSED|SERVFAIL"}}[$__range])) or vector(0)) / {dns_total}', instant=True)],
        unit="percentunit", decimals=2, thresholds=steps((GREEN, None), (ORANGE, 0.01), (RED, 0.05)))
    d.stat(2204, "Unattributed (overflow)", "Share of queries recorded on the OpenTelemetry overflow series because the bounded DNS dimension combinations "
        "exceeded the SDK cardinality limit. Totals stay correct; per-dimension breakdowns below undercount by this share.",
        [prom(f'(sum(increase(cloudflare_dns_queries_total{{{S},otel_metric_overflow="true"}}[$__range])) or vector(0)) / '
              f'sum(increase(cloudflare_dns_queries_total{{{S}}}[$__range]))', instant=True)],
        unit="percentunit", decimals=1, thresholds=steps((GREEN, None), (ORANGE, 0.01), (RED, 0.1)))
    d.stat(2205, "Gateway DNS queries", "Account-level Zero Trust Gateway resolver queries over the selected range.",
        [prom(f'sum(increase(cloudflare_gateway_dns_queries_total{{{S}}}[$__range]))', instant=True)], decimals=0)
    d.stat(2206, "Gateway DNS blocked", "Gateway resolver queries with a block decision over the selected range.",
        [prom(f'sum(increase(cloudflare_gateway_dns_queries_total{{{S},cloudflare_gateway_dns_decision=~"(?i).*block.*"}}[$__range])) or vector(0)', instant=True)],
        decimals=0)

    d.ts(911, "DNS analytics queries by zone", "DNS query rate from dnsAnalyticsAdaptiveGroups for the 10 busiest zones, aggregated over bounded DNS dimensions (no metric colo). "
        "Collector series-cap fallback uses zone <aggregated>; SDK overflow has no zone and is shown as unattributed (overflow).",
        [prom(f'topk(10, label_replace(sum by (cloudflare_dns_zone) (rate(cloudflare_dns_queries_total{{{DNS}}}[$__rate_interval])), '
              f'"cloudflare_dns_zone", "unattributed (overflow)", "cloudflare_dns_zone", ""))', "{{cloudflare_dns_zone}}")],
        unit="reqps", stack=True, legend="table")
    d.donut(2211, "Query types", "Share of queries by record type over the selected range (overflow series excluded).",
        [prom(f'sum by (cloudflare_dns_query_type) (increase(cloudflare_dns_queries_total{{{DNS},cloudflare_dns_query_type!=""}}[$__range]))',
              "{{cloudflare_dns_query_type}}", instant=True)], decimals=0)
    d.ts(2212, "Response codes", "Query rate by DNS response code, aggregated over all other dimensions.",
        [prom(f'sum by (cloudflare_dns_response_code) (rate(cloudflare_dns_queries_total{{{DNS},cloudflare_dns_response_code!=""}}[$__rate_interval]))',
              "{{cloudflare_dns_response_code}}")],
        unit="reqps", stack=True, overrides=color_overrides({"NOERROR": GREEN, "NXDOMAIN": ORANGE, "REFUSED": RED, "SERVFAIL": PURPLE}))
    d.bargauge(2213, "Top answering data centres", "DNS query log events by Cloudflare edge colo, 15 busiest. Raw events, not the Groups metric rate; sampling and caps apply.",
        [loki(f'topk(15, sum by (cloudflare_dns_colo) (count_over_time({LOG} | event_name="cloudflare.dns.query" '
              '| cloudflare_dns_zone=~"$zone" | cloudflare_dns_colo!="" [$__range])))',
              "{{cloudflare_dns_colo}}", instant=True)], decimals=0, transformations=loki_rows("Queries") + [{"id": "rowsToFields", "options": {"mappings": [
                  {"fieldName": "cloudflare_dns_colo", "handlerKey": "field.name"}, {"fieldName": "Queries", "handlerKey": "field.value"}]}}])
    d.donut(2214, "Transport protocol", "UDP versus TCP (and DoH/DoT where reported) over the selected range.",
        [prom(f'sum by (cloudflare_dns_protocol) (increase(cloudflare_dns_queries_total{{{DNS},cloudflare_dns_protocol!=""}}[$__range]))',
              "{{cloudflare_dns_protocol}}", instant=True)], decimals=0)

    d.ts(912, "Gateway DNS queries by decision", "Account-level Gateway DNS query rate by resolver decision.",
        [prom(f'sum by (cloudflare_gateway_dns_decision) (rate(cloudflare_gateway_dns_queries_total{{{S}}}[$__rate_interval]))', "{{cloudflare_gateway_dns_decision}}")],
        unit="reqps", stack=True, overrides=color_overrides({"allow": GREEN, "override": BLUE, "block": RED}))
    d.donut(2221, "Gateway query types", "Gateway resolver queries by record type over the selected range.",
        [prom(f'sum by (cloudflare_gateway_dns_query_type) (increase(cloudflare_gateway_dns_queries_total{{{S}}}[$__range]))',
              "{{cloudflare_gateway_dns_query_type}}", instant=True)], decimals=0)
    d.geomap(2222, "Gateway queries by country", "Where Gateway resolver queries came from, by the country Cloudflare attributes to the client.",
        [table_q(f'sum by (cloudflare_gateway_dns_country) (increase(cloudflare_gateway_dns_queries_total{{{S}}}[$__range]))')],
        country_field="cloudflare_gateway_dns_country", value_field="Value")

    d.logs(2231, "DNS query events", "Per-query events from dnsAnalyticsAdaptive (sampled). Query names and IPs stay on logs.",
        f'{LOG} | event_name="cloudflare.dns.query" | cloudflare_dns_zone=~"$zone"')

    return tab(TAB_DNS, [
        row("Summary", [(2201, 4, 4), (2202, 4, 4), (2203, 4, 4), (2204, 4, 4), (2205, 4, 4), (2206, 4, 4)]),
        row("Authoritative DNS", [(911, 16, 9), (2211, 8, 9), (2212, 10, 9), (2213, 8, 9), (2214, 6, 9)]),
        row("Gateway DNS (Zero Trust resolver)", [(912, 12, 9), (2221, 6, 9), (2222, 6, 9)]),
        row("DNS query logs", [(2231, 24, 12)], collapse=True),
    ])


def access_tab(d: Dashboard) -> dict:
    d.stat(2301, "Human logins", "Exact identity logins from the Access REST log over the selected range, counted from log events.",
        [loki(f'sum(count_over_time({LOG} | event_name="cloudflare.access.login" [$__range]))', instant=True)], decimals=0, no_value="0")
    d.stat(2302, "Denied logins", "REST log login events where Access did not allow the user.",
        [loki(f'sum(count_over_time({LOG} | event_name="cloudflare.access.login" | cloudflare_access_allowed="false" [$__range]))', instant=True)],
        decimals=0, no_value="0")
    d.stat(2303, "Service-token requests", "Nonidentity (service token and bypass) Access requests over the selected range; GraphQL Groups count, "
        "corrected for sampling.",
        [prom(f'sum(increase(cloudflare_access_requests_total{{{S},cloudflare_access_identity_provider="nonidentity"}}[$__range]))', instant=True)], decimals=0)
    d.stat(2304, "Applications", "Access applications in the latest inventory snapshot.",
        [prom(f'count(cloudflare_access_apps_ratio{{{S}}})', instant=True)])
    d.stat(2305, "Users", "Access users in the latest inventory snapshot.",
        [prom(f'max(cloudflare_access_users_ratio{{{S}}})', instant=True)])
    d.gauge(2306, "Identity match ratio", "Share of sampled HTTP events that cf2otel matched to exactly one Access login by IP, host and time. "
        "Rate-based over the current rate interval. Low values are normal for public sites; this is inference, not authentication.",
        [prom(f'sum(rate(cf2otel_identity_outcomes_total{{{S},cf2otel_identity_outcome="matched"}}[$__rate_interval])) / '
              f'sum(rate(cf2otel_identity_outcomes_total{{{S}}}[$__rate_interval]))', instant=True)],
        unit="percentunit", decimals=1, thresholds=NEUTRAL)

    d.ts(103, "Access logins by app and outcome", "Access GraphQL identity login metrics by app and allowed state; REST log counters are excluded to avoid double counting.",
        [prom(f'sum by (cloudflare_access_app, cloudflare_access_allowed) (rate(cloudflare_access_logins_total{{{S},cloudflare_access_identity_provider!="",'
              f'cloudflare_access_identity_provider!="nonidentity"}}[$__rate_interval]))', "{{cloudflare_access_app}} ({{cloudflare_access_allowed}})")],
        unit="reqps", stack=True)
    d.ts(102, "Nonidentity Access requests", "Separate service-token and bypass traffic; GraphQL Groups count is corrected for sampling.",
        [prom(f'sum by (cloudflare_access_service_token) (rate(cloudflare_access_requests_total{{{S},cloudflare_access_identity_provider="nonidentity"}}[$__rate_interval]))',
              "service token={{cloudflare_access_service_token}}")],
        unit="reqps", stack=True)
    d.bargauge(2311, "Logins by application", "REST log login events per Access application over the selected range.",
        [loki(f'sum by (cloudflare_access_app) (count_over_time({LOG} | event_name="cloudflare.access.login" [$__range]))', instant=True)],
        decimals=0, transformations=loki_rows("Logins") + [{"id": "rowsToFields", "options": {"mappings": [
            {"fieldName": "cloudflare_access_app", "handlerKey": "field.name"}, {"fieldName": "Logins", "handlerKey": "field.value"}]}}])
    d.geomap(101, "Login countries", "Where Access logins came from, from the REST log's country field (upper-cased for the map).",
        [loki(f'sum by (country) (count_over_time({LOG} | event_name="cloudflare.access.login" '
              '| label_format country="{{ ToUpper .cloudflare_access_country }}" [$__range]))', instant=True)],
        country_field="country", value_field="Logins", transformations=loki_rows("Logins"))

    d.donut(2321, "Applications by type", "Access application inventory by application type.",
        [prom(f'count by (cloudflare_access_app_type) (cloudflare_access_apps_ratio{{{S}}})', "{{cloudflare_access_app_type}}", instant=True)])
    d.table(2322, "Application inventory", "Every Access application in the latest inventory snapshot.",
        [table_q(f'max by (cloudflare_access_app, cloudflare_access_app_type) (cloudflare_access_apps_ratio{{{S}}})')],
        columns={"cloudflare_access_app": "Application", "cloudflare_access_app_type": "Type"}, hide=["Value"],
        order=["cloudflare_access_app", "cloudflare_access_app_type"])
    d.table(2323, "SCIM provisioning updates", "SCIM updates from the Access SCIM log over the selected range, by resource type, method and HTTP status. "
        "Empty means no identity provider pushed changes in the range.",
        [loki(f'sum by (cloudflare_access_scim_resource_type, cloudflare_access_scim_method, cloudflare_access_scim_status) '
              f'(count_over_time({LOG} | event_name="cloudflare.access.scim_update" [$__range]))', instant=True)],
        pre=loki_rows("Updates"),
        columns={"cloudflare_access_scim_resource_type": "Resource", "cloudflare_access_scim_method": "Method", "cloudflare_access_scim_status": "Status"},
        order=["cloudflare_access_scim_resource_type", "cloudflare_access_scim_method", "cloudflare_access_scim_status", "Updates"], sort_by="Updates")

    d.donut(2331, "HTTP identity inference outcomes", "How cf2otel's identity inference classified sampled HTTP events: matched to one Access login, "
        "unmatched, or ambiguous (several candidates). Counts over the selected range, reset-safe.",
        [prom(f'sum by (cf2otel_identity_outcome) (increase(cf2otel_identity_outcomes_total{{{S}}}[$__range]))', "{{cf2otel_identity_outcome}}", instant=True)],
        overrides=color_overrides({"matched": GREEN, "unmatched": "text", "ambiguous": ORANGE}), decimals=0)
    d.stat(202, "Inferred identity events", "Log events whose Access user match was inferred from IP, host and time; this is not authenticated request identity.",
        [loki(f'sum(count_over_time({LOG} | event_name="cloudflare.http.request" | cloudflare_access_identity_inferred="true" [$__range]))', instant=True)],
        decimals=0, no_value="0")

    d.logs(2341, "Access login events", "REST login events with app, decision, country and available identity details. The source keeps about a day of history.",
        f'{LOG} | event_name="cloudflare.access.login"')
    d.logs(2342, "SCIM update events", "Access SCIM update log entries.", f'{LOG} | event_name="cloudflare.access.scim_update"')

    d.stat(2324, "Access and Gateway seats", "Last observed seat counts from independent Access and Gateway flags; a user may count in both. "
        "These are not additive unique billing users, capacity or entitlement. Maximum across fresh instances avoids summing duplicate pollers; "
        "use for one deployment/account, not a mixed-interval fleet. Missing or stale data is unknown, not zero. "
        f"Requires per-instance last success less than {ACCESS_SEATS_FRESHNESS_SECONDS} seconds old (three generator-configured polling intervals). "
        "If access.seats uses a non-default interval, regenerate with GRAFANA_ACCESS_SEATS_INTERVAL_SECONDS matching the selected deployment "
        "(default 900 seconds). This is not an observed runtime interval or active/health indicator: disabling after a successful poll may leave "
        "last observed counts visible until the freshness threshold.",
        [prom(f'max by (cloudflare_access_seat_type) (cloudflare_access_seats_ratio{{{S},cloudflare_access_seat_type="access"}} '
              f'and on (instance) {ACCESS_SEATS_FRESH})', "Access", instant=True),
         prom(f'max by (cloudflare_access_seat_type) (cloudflare_access_seats_ratio{{{S},cloudflare_access_seat_type="gateway"}} '
              f'and on (instance) {ACCESS_SEATS_FRESH})', "Gateway", ref="B", instant=True)],
        unit="none", decimals=0, text_mode="value_and_name", no_value="No data")

    return tab(TAB_ACCESS, [
        row("Summary", [(2301, 4, 4), (2302, 4, 4), (2303, 4, 4), (2304, 4, 4), (2305, 4, 4), (2306, 4, 4)]),
        row("Logins", [(103, 12, 9), (102, 12, 9), (2311, 12, 9), (101, 12, 9)]),
        row("Inventory and provisioning", [(2321, 6, 9), (2322, 10, 9), (2323, 8, 9)]),
        row("Identity inference", [(2331, 12, 7), (202, 12, 7)]),
        row("Access logs", [(2341, 24, 12), (2342, 24, 8)], collapse=True),
        row("Seat inventory", [(2324, 24, 4)]),
    ])


def ai_tab(d: Dashboard) -> dict:
    d.stat(2401, "Requests", "Completed AI Gateway requests from the REST gateway log over the selected range.",
        [prom(f'sum(increase(cloudflare_ai_gateway_requests_total{{{GW}}}[$__range]))', instant=True)], decimals=0)
    d.stat(302, "AI Gateway cost", "Gateway-reported cost values; currency is unverified, so no currency unit is asserted.",
        [prom(f'sum(increase(cloudflare_ai_gateway_cost_total{{{GW}}}[$__range]))', instant=True)], unit="none", decimals=2)
    d.stat(2402, "Input tokens", "Input tokens over the selected range. Cached input is a subset and is not added.",
        [prom(f'sum(increase(gen_ai_client_inference_usage_input_tokens_total{{{GW}}}[$__range]))', instant=True)], decimals=0)
    d.stat(2403, "Output tokens", "Output tokens over the selected range. Reasoning output is a subset and is not added.",
        [prom(f'sum(increase(gen_ai_client_inference_usage_output_tokens_total{{{GW}}}[$__range]))', instant=True)], decimals=0)
    d.stat(2404, "Error rate", "Requests with a status code >= 400 or an explicit non-success outcome, as a share of all requests.",
        [prom(f'(sum(increase(cloudflare_ai_gateway_errors_total{{{GW}}}[$__range])) or vector(0)) / sum(increase(cloudflare_ai_gateway_requests_total{{{GW}}}[$__range]))', instant=True)],
        unit="percentunit", decimals=2, thresholds=steps((GREEN, None), (ORANGE, 0.01), (RED, 0.05)))
    d.stat(305, "Cache-hit ratio", "Requests served from cache (cloudflare.ai_gateway.cached) over all completed requests.",
        [prom(f'(sum(increase(cloudflare_ai_gateway_cache_hits_total{{{GW}}}[$__range])) or vector(0)) / sum(increase(cloudflare_ai_gateway_requests_total{{{GW}}}[$__range]))', instant=True)],
        unit="percentunit", decimals=1)
    d.stat(306, "Rate-limited (429)", "No metric carries the exact HTTP status; counted from the per-request log's status_code attribute.",
        [loki(f'sum(count_over_time({LOG} | event_name="cloudflare.ai_gateway.request" | cloudflare_ai_gateway_gateway_name=~"$gateway" '
              f'| cloudflare_ai_gateway_status_code="429" [$__range]))', instant=True)], decimals=0, no_value="0", thresholds=ZERO_GOOD_WARN)

    d.ts(301, "AI Gateway requests and errors", "REST-log request counters; errors are a separate series.",
        [prom(f'sum(rate(cloudflare_ai_gateway_requests_total{{{GW}}}[$__rate_interval]))', "requests", ref="A"),
         prom(f'sum(rate(cloudflare_ai_gateway_errors_total{{{GW}}}[$__rate_interval]))', "errors", ref="B")],
        unit="reqps", overrides=color_overrides({"requests": BLUE, "errors": RED}))
    d.ts(309, "AI Gateway failed requests", "Requests with a status code >= 400 or an explicit non-success outcome, by status class. Empty means no failures.",
        [prom(f'sum by (cf2otel_status_class) (rate(cloudflare_ai_gateway_errors_total{{{GW}}}[$__rate_interval]))', "{{cf2otel_status_class}}")],
        unit="reqps", stack=True, overrides=color_overrides(STATUS_COLORS), no_value="No failed requests")
    d.donut(2411, "Requests by operation", "Requests over the selected range by GenAI operation (chat, embeddings and so on).",
        [prom(f'sum by (gen_ai_operation_name) (increase(cloudflare_ai_gateway_requests_total{{{GW}}}[$__range]))', "{{gen_ai_operation_name}}", instant=True)], decimals=0)

    d.heatmap(2412, "Request latency distribution", "Completed request duration from the gen_ai.client.operation.duration histogram. "
        "Brighter cells hold more requests.",
        [prom(f'sum by (le) (increase(gen_ai_client_operation_duration_seconds_bucket{{{GW}}}[$__rate_interval]))', "{{le}}", fmt="heatmap")])
    d.ts(303, "AI Gateway p95 latency", "Completed request duration from REST rows, converted to seconds.",
        [prom(f'histogram_quantile(0.95, sum by (le, gen_ai_request_model) (rate(gen_ai_client_operation_duration_seconds_bucket{{{GW}}}[$__rate_interval])))',
              "{{gen_ai_request_model}}")], unit="s")
    d.ts(2413, "Tokens per request", "Per-request token counts from the gen_ai.client.inference.operation token histograms: median and p95 for input and output.",
        [prom(f'histogram_quantile(0.5, sum by (le) (rate(gen_ai_client_inference_operation_input_tokens_bucket{{{GW}}}[$__rate_interval])))', "input p50", ref="A"),
         prom(f'histogram_quantile(0.95, sum by (le) (rate(gen_ai_client_inference_operation_input_tokens_bucket{{{GW}}}[$__rate_interval])))', "input p95", ref="B"),
         prom(f'histogram_quantile(0.5, sum by (le) (rate(gen_ai_client_inference_operation_output_tokens_bucket{{{GW}}}[$__rate_interval])))', "output p50", ref="C"),
         prom(f'histogram_quantile(0.95, sum by (le) (rate(gen_ai_client_inference_operation_output_tokens_bucket{{{GW}}}[$__rate_interval])))', "output p95", ref="D")],
        unit="short", decimals=0)

    d.ts(304, "AI Gateway token usage", "Input and output totals are separate; cached and reasoning subsets are not added to totals.",
        [prom(f'sum(rate(gen_ai_client_inference_usage_input_tokens_total{{{GW}}}[$__rate_interval]))', "input", ref="A"),
         prom(f'sum(rate(gen_ai_client_inference_usage_output_tokens_total{{{GW}}}[$__rate_interval]))', "output", ref="B"),
         prom(f'sum(rate(gen_ai_client_inference_usage_cache_read_input_tokens_total{{{GW}}}[$__rate_interval]))', "cached input (subset of input)", ref="C"),
         prom(f'sum(rate(gen_ai_client_inference_usage_reasoning_output_tokens_total{{{GW}}}[$__rate_interval]))', "reasoning (subset of output)", ref="D")],
        unit="short", overrides=[by_regexp(".*subset.*", custom={"lineStyle": {"fill": "dash", "dash": [10, 10]}, "fillOpacity": 0})])
    d.ts(307, "AI Gateway cost by provider", "Gateway-reported cost values by provider; currency is unverified, so no currency unit is asserted.",
        [prom(f'sum by (gen_ai_provider_name) (rate(cloudflare_ai_gateway_cost_total{{{GW}}}[$__rate_interval]))', "{{gen_ai_provider_name}}")],
        unit="none", stack=True)
    d.donut(2414, "Cost share by model", "Share of gateway-reported cost over the selected range by model. Currency is unverified.",
        [prom(f'sum by (gen_ai_request_model) (increase(cloudflare_ai_gateway_cost_total{{{GW}}}[$__range]))', "{{gen_ai_request_model}}", instant=True)],
        unit="none", decimals=2)
    d.table(308, "AI Gateway usage by model", "Request count, token usage and cost over the dashboard range, by model and provider. "
        "Cached input and reasoning output are subsets of input and output.",
        [table_q(f'sum by (gen_ai_request_model, gen_ai_provider_name) (increase(cloudflare_ai_gateway_requests_total{{{GW}}}[$__range]))', "A"),
         table_q(f'sum by (gen_ai_request_model, gen_ai_provider_name) (increase(gen_ai_client_inference_usage_input_tokens_total{{{GW}}}[$__range]))', "B"),
         table_q(f'sum by (gen_ai_request_model, gen_ai_provider_name) (increase(gen_ai_client_inference_usage_cache_read_input_tokens_total{{{GW}}}[$__range]))', "C"),
         table_q(f'sum by (gen_ai_request_model, gen_ai_provider_name) (increase(gen_ai_client_inference_usage_output_tokens_total{{{GW}}}[$__range]))', "D"),
         table_q(f'sum by (gen_ai_request_model, gen_ai_provider_name) (increase(gen_ai_client_inference_usage_reasoning_output_tokens_total{{{GW}}}[$__range]))', "E"),
         table_q(f'sum by (gen_ai_request_model, gen_ai_provider_name) (increase(cloudflare_ai_gateway_cost_total{{{GW}}}[$__range]))', "F")],
        columns={"gen_ai_request_model": "Model", "gen_ai_provider_name": "Provider", "Value #A": "Requests", "Value #B": "Input tokens",
                 "Value #C": "Cached input", "Value #D": "Output tokens", "Value #E": "Reasoning output", "Value #F": "Cost"},
        order=["gen_ai_request_model", "gen_ai_provider_name", "Value #A", "Value #B", "Value #C", "Value #D", "Value #E", "Value #F"],
        sort_by="Requests", decimals=0,
        overrides=[by_name("Cost", unit="none", decimals=4), by_name("Model", custom={"width": 280, "align": "auto", "cellOptions": {"type": "auto"}}),
                   by_name("Requests", custom={"cellOptions": {"type": "gauge", "mode": "basic", "valueDisplayMode": "text"}, "align": "auto"},
                           color={"mode": "continuous-BlPu"})])

    d.ts(310, "AI Gateway DLP-flagged requests", "Requests with a flagged DLP outcome, split by request/response direction; blocked and other outcomes are excluded.",
        [prom(f'sum by (cloudflare_ai_gateway_dlp_direction) (rate(cloudflare_ai_gateway_dlp_requests_total{{{GW},cloudflare_ai_gateway_dlp_action="flagged"}}[$__rate_interval]))',
              "{{cloudflare_ai_gateway_dlp_direction}}")], unit="reqps", stack=True, no_value="No DLP flags")
    d.donut(2421, "DLP outcomes", "Requests with any DLP outcome over the selected range, by action (flagged, blocked, other) and direction. Empty means DLP matched nothing.",
        [prom(f'sum by (cloudflare_ai_gateway_dlp_action, cloudflare_ai_gateway_dlp_direction) (increase(cloudflare_ai_gateway_dlp_requests_total{{{GW}}}[$__range]))',
              "{{cloudflare_ai_gateway_dlp_action}} {{cloudflare_ai_gateway_dlp_direction}}", instant=True)], decimals=0)
    d.ts(2422, "REST log coverage gap", "Requests counted by aiGatewayRequestsAdaptiveGroups minus requests in the REST gateway log, per closed five-minute window. "
        "Positive means the REST log (and so every panel above) is missing requests; zero means the sources agree; negative means Groups is behind. "
        "Needs the aigateway.coverage collector, which is off by default.",
        [prom(f'sum by (cloudflare_ai_gateway_gateway_name) (cloudflare_ai_gateway_log_coverage_gap{{{GW}}})', "{{cloudflare_ai_gateway_gateway_name}}")],
        unit="short", bars=True, min_zero=False, decimals=0, no_value="Coverage collector off or no data")

    d.ts(2423, "Non-JSON bodies omitted", "Non-JSON bodies fetched during opt-in body capture, counted by request/response side over each interval. "
        "Content is omitted, but request metadata continues to export. This counter has no gateway dimension: it covers all gateways, "
        "regardless of the gateway filter. One request can contribute both sides. Request logs and traces carry body_non_json flags.",
        [prom(f'sum by (cloudflare_ai_gateway_body_side) (increase(cloudflare_ai_gateway_body_non_json_total{{{S}}}[$__interval]))',
              "{{cloudflare_ai_gateway_body_side}}")],
        unit="short", bars=True, decimals=0, interval="5m", no_value="Body capture off or no non-JSON bodies")

    d.panel(2431, "Recent GenAI traces", "GenAI client spans that cf2otel builds from the AI Gateway REST log, newest first. Open a trace for model, "
        "provider, token usage, cost and timing attributes.", "table",
        [tempo('{resource.service.name="cf2otel" && span.gen_ai.operation.name != nil && span.cloudflare.ai_gateway.gateway.name =~ "${gateway:regex}"}')],
        options={"showHeader": True, "cellHeight": "sm", "footer": {"show": False, "reducer": ["sum"], "countRows": False}},
        defaults={"custom": {"align": "auto", "cellOptions": {"type": "auto"}, "filterable": False}})

    d.logs(2441, "AI Gateway request logs", "Per-request REST log metadata and outcome. Request and response content is optional and capped.",
        f'{LOG} | event_name="cloudflare.ai_gateway.request" | cloudflare_ai_gateway_gateway_name=~"$gateway"')
    d.logs(2442, "GenAI content logs", "Opt-in request and response bodies, one log per available side, subject to the body cap. Empty unless body capture is enabled.",
        f'{LOG} | event_name="gen_ai.client.inference.operation.details" | cloudflare_ai_gateway_gateway_name=~"$gateway"')

    return tab(TAB_AI, [
        row("Summary", [(2401, 4, 4), (302, 3, 4), (2402, 4, 4), (2403, 4, 4), (2404, 3, 4), (305, 3, 4), (306, 3, 4)]),
        row("Traffic", [(301, 10, 8), (309, 8, 8), (2411, 6, 8)]),
        row("Latency", [(2412, 12, 9), (303, 12, 9), (2413, 24, 7)]),
        row("Tokens and cost", [(304, 12, 8), (307, 12, 8), (308, 16, 8), (2414, 8, 8)]),
        row("DLP and log coverage", [(310, 9, 8), (2421, 7, 8), (2422, 8, 8)]),
        row("Body capture data quality (all gateways)", [(2423, 24, 7)]),
        row("Traces", [(2431, 24, 9)]),
        row("AI Gateway logs", [(2441, 24, 12), (2442, 24, 10)], collapse=True),
    ])


# Google's published Core Web Vitals thresholds: good at or below the first value, poor above the second.
VITALS = [
    ("lcp", "LCP", "cloudflare_rum_lcp_p75_seconds", "s", 2.5, 4.0, 922, "Rolling p75 largest contentful paint, exported in seconds after the collector converts GraphQL microseconds."),
    ("inp", "INP", "cloudflare_rum_inp_p75_seconds", "s", 0.2, 0.5, 923, "Rolling p75 interaction to next paint, exported in seconds after the collector converts GraphQL microseconds."),
    ("cls", "CLS", "cloudflare_rum_cls_p75_ratio", "none", 0.1, 0.25, 927, "Rolling p75 cumulative layout shift score gauge."),
    ("fcp", "FCP", "cloudflare_rum_fcp_p75_seconds", "s", 1.8, 3.0, 925, "Rolling p75 first contentful paint, exported in seconds after the collector converts GraphQL microseconds."),
    ("ttfb", "TTFB", "cloudflare_rum_ttfb_p75_seconds", "s", 0.8, 1.8, 926, "Rolling p75 time to first byte, exported in seconds after the collector converts GraphQL microseconds."),
]


def web_tab(d: Dashboard) -> dict:
    d.stat(2501, "Page views", "Page loads measured by Web Analytics over the selected range.",
        [prom(f'sum(increase(cloudflare_rum_page_views_total{{{RUM}}}[$__range]))', instant=True)], decimals=0)
    d.stat(2502, "Visits", "Visits (sessions) measured by Web Analytics over the selected range.",
        [prom(f'sum(increase(cloudflare_rum_sessions_total{{{RUM}}}[$__range]))', instant=True)], decimals=0)
    stat_ids = []
    for i, (_key, name, metric, unit, good, poor, _ts_id, _desc) in enumerate(VITALS):
        pid = 2503 + i
        stat_ids.append(pid)
        d.stat(pid, f"{name} p75", f"Highest rolling p75 {name} reported in the selected range across the selected sites and devices. "
            f"Google thresholds: good up to {good}, poor above {poor}{'s' if unit == 's' else ''}. Windows without samples report 0, so the "
            "latest value alone would read as falsely good.",
            [prom(f'max(max_over_time({metric}{{{RUM}}}[$__range]))', instant=True)],
            unit=unit, decimals=2 if unit == "s" else 3, thresholds=steps((GREEN, None), (ORANGE, good + 1e-9), (RED, poor + 1e-9)),
            color_mode="background", no_value="No samples")

    d.ts(920, "RUM page views", "Page view rate from rumPageloadEventsAdaptiveGroups by device.",
        [prom(f'sum by (cloudflare_rum_device_type) (rate(cloudflare_rum_page_views_total{{{RUM}}}[$__rate_interval]))', "{{cloudflare_rum_device_type}}")],
        unit="reqps", stack=True, bars=True, interval="15m")
    d.ts(921, "RUM sessions", "Visit rate from rumPageloadEventsAdaptiveGroups by device.",
        [prom(f'sum by (cloudflare_rum_device_type) (rate(cloudflare_rum_sessions_total{{{RUM}}}[$__rate_interval]))', "{{cloudflare_rum_device_type}}")],
        unit="reqps", stack=True, bars=True, interval="15m")
    d.donut(2511, "Device split", "Page views by device type over the selected range.",
        [prom(f'sum by (cloudflare_rum_device_type) (increase(cloudflare_rum_page_views_total{{{RUM}}}[$__range]))', "{{cloudflare_rum_device_type}}", instant=True)], decimals=0)
    d.geomap(2512, "Visitors by country", "Page views by visitor country over the selected range.",
        [table_q(f'sum by (cloudflare_rum_country) (increase(cloudflare_rum_page_views_total{{{RUM}}}[$__range]))')],
        country_field="cloudflare_rum_country", value_field="Value")
    d.bargauge(2513, "Top countries", "The 10 countries with the most page views over the selected range.",
        [prom(f'sort_desc(topk(10, sum by (cloudflare_rum_country) (increase(cloudflare_rum_page_views_total{{{RUM}}}[$__range]))))', "{{cloudflare_rum_country}}", instant=True)],
        decimals=0)

    queries, columns, order, overrides = [], {"cloudflare_rum_site_tag": "Site tag", "cloudflare_rum_device_type": "Device"}, ["cloudflare_rum_site_tag", "cloudflare_rum_device_type"], []
    for ref, (_key, name, metric, unit, good, poor, _ts_id, _desc) in zip("ABCDE", VITALS):
        queries.append(table_q(f'max by (cloudflare_rum_site_tag, cloudflare_rum_device_type) (max_over_time({metric}{{{RUM}}}[$__range]))', ref))
        columns[f"Value #{ref}"] = name
        order.append(f"Value #{ref}")
        overrides.append(by_name(name, unit=unit, decimals=2 if unit == "s" else 3,
            thresholds=steps((GREEN, None), (ORANGE, good + 1e-9), (RED, poor + 1e-9)),
            custom={"cellOptions": {"type": "color-background", "mode": "basic"}, "align": "center"}))
    d.table(2514, "Web Vitals by site and device", "Highest rolling p75 in the selected range for each Core Web Vital per Web Analytics site tag and device, coloured by "
        "Google's good / needs improvement / poor thresholds. Blank cells had no samples in the range.", queries, columns=columns, order=order, overrides=overrides)

    trend_ids = []
    for _key, name, metric, unit, good, poor, ts_id, desc in VITALS:
        trend_ids.append(ts_id)
        d.ts(ts_id, f"RUM {name} p75", desc + " Dashed lines mark Google's good and poor thresholds.",
            [prom(f'max by (cloudflare_rum_site_tag, cloudflare_rum_device_type) ({metric}{{{RUM}}})',
                  "{{cloudflare_rum_site_tag}} {{cloudflare_rum_device_type}}")],
            unit=unit, thresholds=steps((GREEN, None), (ORANGE, good), (RED, poor)), threshold_style="dashed", interval=None)
    d.ts(924, "RUM FID p75", "Rolling p75 first input delay, converted from GraphQL microseconds to seconds. Google replaced FID with INP in 2024 "
        "and Cloudflare rarely reports it now, so an empty panel is expected.",
        [prom(f'max by (cloudflare_rum_site_tag, cloudflare_rum_device_type) (cloudflare_rum_fid_p75_seconds{{{RUM}}})',
              "{{cloudflare_rum_site_tag}} {{cloudflare_rum_device_type}}")],
        unit="s", thresholds=steps((GREEN, None), (ORANGE, 0.1), (RED, 0.3)), threshold_style="dashed", interval=None, no_value="No FID samples")
    trend_ids.append(924)

    return tab(TAB_WEB, [
        row("Summary", [(2501, 4, 4), (2502, 4, 4)] + [(pid, 3, 4) for pid in stat_ids[:4]] + [(stat_ids[4], 4, 4)]),
        row("Audience", [(920, 12, 8), (921, 12, 8), (2512, 12, 10), (2513, 6, 10), (2511, 6, 10)]),
        row("Core Web Vitals", [(2514, 24, 7)] + [(pid, 8, 7) for pid in trend_ids]),
    ])


def platform_tab(d: Dashboard) -> dict:
    glance = [
        (2601, "Workers requests", f'cloudflare_workers_requests_total{{{WRK}}}', "Workers invocations for the selected scripts."),
        (2602, "D1 queries", f'cloudflare_d1_queries_total{{{S}}}', "D1 query count from d1QueriesAdaptiveGroups; query text is never selected."),
        (2603, "KV requests", f'cloudflare_kv_requests_total{{{S}}}', "KV operation requests, account aggregate."),
        (2604, "R2 requests", f'cloudflare_r2_requests_total{{{R2}}}', "R2 operation requests for the selected buckets."),
        (2605, "Durable Objects requests", f'cloudflare_durableobjects_requests_total{{{S}}}', "Durable Objects invocations, account aggregate."),
        (2606, "Queue operations", f'cloudflare_queues_message_operations_total{{{S}}}', "Queue message operations, account aggregate."),
        (2607, "Logpush uploads", f'cloudflare_logpush_uploads_total{{{S}}}', "Logpush upload batches, account aggregate."),
        (2608, "Emails routed", f'cloudflare_email_routing_events_total{{{S}}}', "Email Routing events summed across account zones."),
    ]
    for pid, title, series, desc in glance:
        d.stat(pid, title, desc + " Total over the selected range.", [prom(f'sum(increase({series}[$__range]))', instant=True)], decimals=0)

    d.ts(501, "Workers requests by script", "Request rate from workersOverviewRequestsAdaptiveGroups for the 10 busiest scripts.",
        [prom(f'topk(10, sum by (cloudflare_workers_script_name) (rate(cloudflare_workers_requests_total{{{WRK}}}[$__rate_interval])))', "{{cloudflare_workers_script_name}}")],
        unit="reqps", stack=True, legend="table")
    d.bargauge(2611, "Top scripts", "Requests per Worker script over the selected range.",
        [prom(f'sort_desc(topk(15, sum by (cloudflare_workers_script_name) (increase(cloudflare_workers_requests_total{{{WRK}}}[$__range]))))',
              "{{cloudflare_workers_script_name}}", instant=True)], decimals=0)

    d.ts(601, "D1 read and write queries", "Read and write query rates from d1AnalyticsAdaptiveGroups, account aggregate.",
        [prom(f'sum(rate(cloudflare_d1_read_queries_total{{{S}}}[$__rate_interval]))', "reads", ref="A"),
         prom(f'sum(rate(cloudflare_d1_write_queries_total{{{S}}}[$__rate_interval]))', "writes", ref="B")],
        unit="reqps", overrides=color_overrides({"reads": BLUE, "writes": ORANGE}))
    d.ts(603, "D1 queries", "Query rate from d1QueriesAdaptiveGroups; query text is never selected or emitted.",
        [prom(f'sum(rate(cloudflare_d1_queries_total{{{S}}}[$__rate_interval]))', "queries")], unit="reqps", legend="hidden")
    d.stat(604, "D1 storage", "Largest D1 database size in the latest complete bucket.",
        [prom(f'max(last_over_time(cloudflare_d1_storage_max_database_bytes{{{S}}}[$__range]))', instant=True)], unit="bytes", decimals=1)
    d.ts(605, "KV requests", "Request rate from kvOperationsAdaptiveGroups.",
        [prom(f'sum(rate(cloudflare_kv_requests_total{{{S}}}[$__rate_interval]))', "requests")], unit="reqps", legend="hidden")
    d.stat(606, "KV storage bytes", "Largest KV namespace size in the latest complete bucket.",
        [prom(f'max(last_over_time(cloudflare_kv_storage_max_namespace_bytes{{{S}}}[$__range]))', instant=True)], unit="bytes", decimals=1)
    d.stat(607, "KV storage keys", "Largest KV namespace key count in the latest complete bucket.",
        [prom(f'max(last_over_time(cloudflare_kv_storage_max_namespace_keys{{{S}}}[$__range]))', instant=True)], decimals=0)

    d.ts(705, "R2 requests by bucket", "Request rate from r2OperationsAdaptiveGroups, grouped by bucket.",
        [prom(f'sum by (cloudflare_r2_bucket_name) (rate(cloudflare_r2_requests_total{{{R2}}}[$__rate_interval]))', "{{cloudflare_r2_bucket_name}}")],
        unit="reqps", stack=True)
    d.ts(701, "R2 bandwidth by bucket", "Download (above zero) and upload (below zero) byte rates from r2BandwidthUsageAdaptiveGroups, by bucket.",
        [prom(f'sum by (cloudflare_r2_bucket_name) (rate(cloudflare_r2_bandwidth_download_bytes_total{{{R2}}}[$__rate_interval]))', "{{cloudflare_r2_bucket_name}} download", ref="A"),
         prom(f'sum by (cloudflare_r2_bucket_name) (rate(cloudflare_r2_bandwidth_upload_bytes_total{{{R2}}}[$__rate_interval]))', "{{cloudflare_r2_bucket_name}} upload", ref="B")],
        unit="Bps", min_zero=False, overrides=[by_regexp(".* upload", custom={"transform": "negative-Y"})])
    d.bargauge(706, "R2 storage by bucket", "Payload bytes per bucket in the latest complete bucket.",
        [prom(f'sort_desc(max by (cloudflare_r2_bucket_name) (last_over_time(cloudflare_r2_storage_payload_bytes{{{R2}}}[$__range])))', "{{cloudflare_r2_bucket_name}}", instant=True)],
        unit="bytes", decimals=1)
    d.bargauge(707, "R2 objects by bucket", "Object count per bucket in the latest complete bucket.",
        [prom(f'sort_desc(max by (cloudflare_r2_bucket_name) (last_over_time(cloudflare_r2_storage_objects{{{R2}}}[$__range])))', "{{cloudflare_r2_bucket_name}}", instant=True)],
        decimals=0)
    d.ts(703, "R2 Data Catalog activity", "Catalog data operations and table maintenance jobs by namespace. Empty unless R2 Data Catalog is in use.",
        [prom(f'sum by (cloudflare_r2_catalog_namespace_name) (rate(cloudflare_r2_catalog_data_operations_total{{{S}}}[$__rate_interval]))', "data operations {{cloudflare_r2_catalog_namespace_name}}", ref="A"),
         prom(f'sum by (cloudflare_r2_catalog_namespace_name) (rate(cloudflare_r2_catalog_maintenance_jobs_total{{{S}}}[$__rate_interval]))', "maintenance jobs {{cloudflare_r2_catalog_namespace_name}}", ref="B")],
        unit="reqps", no_value="No Data Catalog activity")
    d.ts(708, "R2 SQL queries", "Query rate from r2sqlOperationsAdaptiveGroups, grouped by bucket. Empty unless R2 SQL is in use.",
        [prom(f'sum by (cloudflare_r2sql_bucket_name) (rate(cloudflare_r2sql_queries_total{{{S},cloudflare_r2sql_bucket_name=~"$bucket"}}[$__rate_interval]))', "{{cloudflare_r2sql_bucket_name}}")],
        unit="reqps", no_value="No R2 SQL activity")

    d.ts(801, "Durable Objects requests", "Invocation requests (durableObjectsInvocationsAdaptiveGroups) and periodic subrequests (durableObjectsPeriodicGroups), account aggregate.",
        [prom(f'sum(rate(cloudflare_durableobjects_requests_total{{{S}}}[$__rate_interval]))', "requests", ref="A"),
         prom(f'sum(rate(cloudflare_durableobjects_subrequests_total{{{S}}}[$__rate_interval]))', "subrequests", ref="B")], unit="reqps")
    d.ts(804, "Durable Objects request body bytes", "Uncached request body byte rate from durableObjectsSubrequestsAdaptiveGroups.",
        [prom(f'sum(rate(cloudflare_durableobjects_subrequests_request_body_bytes_total{{{S}}}[$__rate_interval]))', "request body")], unit="Bps", legend="hidden")
    d.stat(803, "Durable Objects SQL storage", "Largest Durable Objects namespace SQL storage in the latest complete bucket.",
        [prom(f'max(last_over_time(cloudflare_durableobjects_sql_storage_max_namespace_bytes{{{S}}}[$__range]))', instant=True)], unit="bytes", decimals=1)

    d.ts(905, "Queues message operations", "Message and billable operation rates from queueMessageOperationsAdaptiveGroups, account aggregate.",
        [prom(f'sum(rate(cloudflare_queues_message_operations_total{{{S}}}[$__rate_interval]))', "operations", ref="A"),
         prom(f'sum(rate(cloudflare_queues_message_billable_operations_total{{{S}}}[$__rate_interval]))', "billable operations", ref="B")], unit="reqps")
    d.ts(901, "Queues backlog messages", "Largest per-queue average backlog and delayed backlog in the latest complete bucket. A backlog that keeps rising means consumers are falling behind.",
        [prom(f'max(cloudflare_queues_backlog_max_queue_avg_messages{{{S}}})', "backlog", ref="A"),
         prom(f'max(cloudflare_queues_delayed_backlog_max_queue_avg_messages{{{S}}})', "delayed backlog", ref="B")],
        unit="short", interval=None)
    d.ts(902, "Queues backlog bytes", "Largest per-queue average backlog size in the latest complete bucket.",
        [prom(f'max(cloudflare_queues_backlog_max_queue_avg_bytes{{{S}}})', "backlog bytes")], unit="bytes", legend="hidden", interval=None)
    d.ts(903, "Queues consumer concurrency", "Largest per-queue average consumer concurrency in the latest complete bucket.",
        [prom(f'max(cloudflare_queues_consumer_max_queue_avg_concurrency{{{S}}})', "concurrency")], unit="short", legend="hidden", interval=None)

    d.ts(502, "Turnstile events", "Event rate from turnstileAdaptiveGroups, account aggregate.",
        [prom(f'sum(rate(cloudflare_turnstile_events_total{{{S}}}[$__rate_interval]))', "events")], unit="reqps", legend="hidden")
    d.ts(503, "Logpush health", "Upload and record rates from logpushHealthAdaptiveGroups, account aggregate.",
        [prom(f'sum(rate(cloudflare_logpush_uploads_total{{{S}}}[$__rate_interval]))', "uploads", ref="A"),
         prom(f'sum(rate(cloudflare_logpush_records_total{{{S}}}[$__rate_interval]))', "records", ref="B")],
        unit="reqps", overrides=[by_name("records", custom={"axisPlacement": "right"})])
    d.ts(2650, "Logpush failed uploads by job and destination", "Opt-in logpush.failures. Failure rates by scope, zone, job, destination, status and final attempt. Final-attempt status >=300 means final loss; earlier attempts may recover.",
        [prom(f'sum by (cloudflare_logpush_scope, cloudflare_logpush_zone, cloudflare_logpush_job_id, cloudflare_logpush_destination_type, cloudflare_logpush_status_code, cloudflare_logpush_final_attempt) (rate(cloudflare_logpush_failed_uploads_total{{{S}}}[$__rate_interval]))',
              "{{cloudflare_logpush_scope}} {{cloudflare_logpush_zone}} job {{cloudflare_logpush_job_id}} / {{cloudflare_logpush_destination_type}} / {{cloudflare_logpush_status_code}} final={{cloudflare_logpush_final_attempt}}")], unit="ops", legend="table")

    d.ts(2651, "Durable Objects errors by script", "Invocation errors by bounded script name.",
        [prom(f'sum by (cloudflare_workers_script_name) (rate(cloudflare_durableobjects_errors_total{{{WRK}}}[$__rate_interval]))', "{{cloudflare_workers_script_name}}")], unit="ops")
    for pid, title, metric, unit in (
        (2652, "Durable Objects wall-time statistics", "cloudflare_durableobjects_wall_time_seconds", "s"),
        (2653, "Durable Objects response-size statistics", "cloudflare_durableobjects_response_size_bytes", "bytes"),
        (2655, "D1 query batch-time statistics", "cloudflare_d1_query_batch_time_seconds", "s"),
        (2656, "D1 query batch response-size statistics", "cloudflare_d1_query_batch_response_size_bytes", "bytes"),
    ):
        selector = WRK if pid in (2652, 2653) else S
        d.ts(pid, title, "Latest complete five-minute bucket p50/p75/p99/p999; per script for Durable Objects, account aggregate for D1. Not fleet-wide percentiles; stale observations may persist after polling errors.",
            [prom(f'{metric}{{{selector}}}', "{{cloudflare_workers_script_name}} {{cloudflare_statistic}}")], unit=unit, legend="table")
    d.ts(2654, "D1 rows read and written", "Account aggregate row rates; no database or query identifiers.",
        [prom(f'sum(rate(cloudflare_d1_rows_read_total{{{S}}}[$__rate_interval]))', "read", ref="A"),
         prom(f'sum(rate(cloudflare_d1_rows_written_total{{{S}}}[$__rate_interval]))', "written", ref="B")], unit="ops")
    d.ts(2657, "Queues maximum per-queue average lag", "ReadMessage only: maximum of per-queue average lag in the latest observed complete bucket, not an account-wide average. Missing/N/A is omitted; stale observations may persist.",
        [prom(f'max(cloudflare_queues_message_max_queue_avg_lag_seconds{{{S}}})', "lag")], unit="s", interval=None)
    d.ts(2658, "Queues maximum per-queue average retries", "ReadMessage only: maximum of per-queue average retries. Missing/negative N/A omitted, real zero retained; stale observations may persist.",
        [prom(f'max(cloudflare_queues_message_max_queue_avg_retries{{{S}}})', "retries")], interval=None)
    d.ts(2659, "Queues billable operations by action", "Billable operation rates split by action, consumer type and outcome; no queue identifiers.",
        [prom(f'sum by (cloudflare_queues_action_type, cloudflare_queues_consumer_type, cloudflare_queues_outcome) (rate(cloudflare_queues_message_billable_operations_by_action_total{{{S}}}[$__rate_interval]))',
              "{{cloudflare_queues_action_type}} / {{cloudflare_queues_consumer_type}} / {{cloudflare_queues_outcome}}")], unit="ops", legend="table")
    hc = S + ',cloudflare_health_check_zone=~"$zone"'
    d.ts(2660, "Health-check events by status and reason", "Pro-only opt-in healthchecks.events; event counts are separate from origin timing observations.",
        [prom(f'sum by (cloudflare_health_check_zone, cloudflare_health_check_status, cloudflare_health_check_failure_reason) (rate(cloudflare_health_check_events_total{{{hc}}}[$__rate_interval]))',
              "{{cloudflare_health_check_zone}} {{cloudflare_health_check_status}} / {{cloudflare_health_check_failure_reason}}")], unit="ops", legend="table")
    for pid, title, metric in ((2661, "Health-check RTT", "rtt"), (2662, "Health-check TTFB", "ttfb"),
                              (2663, "Health-check TCP connection", "tcp_connection"), (2664, "Health-check TLS handshake", "tls_handshake")):
        d.ts(pid, title, "Latest complete origin bucket average in seconds, not event-weighted or grouped by status/reason. Pro-only opt-in; stale observations may persist after polling errors.",
            [prom(f'cloudflare_health_check_{metric}_seconds{{{hc}}}', "{{cloudflare_health_check_zone}} / {{cloudflare_health_check_origin}}")], unit="s", legend="table")

    d.ts(2621, "Email Routing and Sending", "Email Routing and Email Sending event rates summed across account-owned zones.",
        [prom(f'sum(rate(cloudflare_email_routing_events_total{{{S}}}[$__rate_interval]))', "routing", ref="A"),
         prom(f'sum(rate(cloudflare_email_sending_events_total{{{S}}}[$__rate_interval]))', "sending", ref="B")], unit="reqps")

    d.ts(2630, "Workers invocations by script and status", "Sample-corrected workersInvocationsAdaptive counts; separate from the overview request dataset.",
        [prom(f'sum by (cloudflare_workers_script_name, cloudflare_workers_status) (rate(cloudflare_workers_invocations_total{{{WRK}}}[$__rate_interval]))',
              "{{cloudflare_workers_script_name}} {{cloudflare_workers_status}}")], unit="reqps")
    for pid, title, metric in ((2631, "Workers errors", "cloudflare_workers_errors_total"), (2632, "Workers subrequests", "cloudflare_workers_subrequests_total")):
        d.ts(pid, title, "Sample-corrected invocation Groups count by script.",
            [prom(f'sum by (cloudflare_workers_script_name) (rate({metric}{{{WRK}}}[$__rate_interval]))', "{{cloudflare_workers_script_name}}")], unit="ops")
    for pid, title, metric in ((2633, "Workers CPU statistics", "cloudflare_workers_cpu_time_seconds"),
                              (2634, "Workers wall-time statistics", "cloudflare_workers_wall_time_seconds"),
                              (2635, "Workers request-duration statistics", "cloudflare_workers_request_duration_seconds")):
        d.ts(pid, title, "Per-script window p50/p75/p99/p999 in seconds, not aggregate percentiles. Workers GB*s duration fields are not exported.",
            [prom(f'{metric}{{{WRK}}}', "{{cloudflare_workers_script_name}} {{cloudflare_statistic}}")], unit="s", legend="table")
    d.table(2640, "Current certificate expiry",
        "Disabled by default. Latest atomic snapshot, excluding packs without a parsable expiry; removed packs and prior statuses retire. "
        "Seconds to earliest expiry are observed at the last successful poll, not a live countdown. "
        f"Requires collector last success less than {CERTS_PACKS_FRESHNESS_SECONDS} seconds old (three polling intervals). "
        "Expiry under 14 days includes already expired packs. No data is not proof of health. "
        "If certs.packs uses a non-default interval, regenerate with GRAFANA_CERTS_PACKS_INTERVAL_SECONDS matching the deployment.",
        [table_q(f'(cloudflare_certificate_expiry_seconds{{{CERT}}} and cloudflare_certificate_pack{{{CERT}}} == 1) and on(instance) {CERT_FRESH}')],
        columns={"cloudflare_certificate_zone": "Zone", "cloudflare_certificate_pack_id": "Pack", "cloudflare_certificate_type": "Type",
                 "cloudflare_certificate_authority": "Authority", "cloudflare_certificate_status": "Status", "Value": "Observed seconds to expiry"},
        order=["cloudflare_certificate_zone", "cloudflare_certificate_pack_id", "cloudflare_certificate_type", "cloudflare_certificate_authority", "cloudflare_certificate_status", "Value"],
        overrides=[by_name("Observed seconds to expiry", unit="s", decimals=0)])
    d.table(2645, "Current certificate pack status",
        "Disabled by default. Present packs from the latest atomic snapshot, including pending packs with unknown expiry. "
        f"Collector last success must be less than {CERTS_PACKS_FRESHNESS_SECONDS} seconds old (three polling intervals). "
        "Non-active status alerts read pack presence, not expiry. Removed packs and old status series retire; no data is not proof of health.",
        [table_q(f'(cloudflare_certificate_pack{{{CERT}}} == 1) and on(instance) {CERT_FRESH}')],
        columns={"cloudflare_certificate_zone": "Zone", "cloudflare_certificate_pack_id": "Pack", "cloudflare_certificate_type": "Type",
                 "cloudflare_certificate_authority": "Authority", "cloudflare_certificate_status": "Status"},
        hide=["Value"], order=["cloudflare_certificate_zone", "cloudflare_certificate_pack_id", "cloudflare_certificate_type", "cloudflare_certificate_authority", "cloudflare_certificate_status"])
    d.table(2641, "Current tunnel status", "Disabled by default. Only value-1 status series are current; retired status series have value zero and are excluded.",
        [table_q(f'cloudflare_tunnel_status{{{S}}} == 1')],
        columns={"cloudflare_tunnel_name": "Tunnel", "cloudflare_tunnel_id": "ID", "cloudflare_tunnel_status": "Status"},
        hide=["Value"], order=["cloudflare_tunnel_name", "cloudflare_tunnel_id", "cloudflare_tunnel_status"])
    d.ts(2642, "Tunnel active connections by colo", "Active connections; retired colo series contribute zero.",
        [prom(f'sum by (cloudflare_tunnel_name, cloudflare_tunnel_id, cloudflare_tunnel_colo) (cloudflare_tunnel_connections{{{S}}})', "{{cloudflare_tunnel_name}} {{cloudflare_tunnel_colo}}")], interval="1m")
    d.ts(2643, "Tunnel connectors by version", "Connector counts; retired version series contribute zero.",
        [prom(f'sum by (cloudflare_tunnel_name, cloudflare_tunnel_id, cloudflare_tunnel_connector_version) (cloudflare_tunnel_connectors{{{S}}})', "{{cloudflare_tunnel_name}} {{cloudflare_tunnel_connector_version}}")], interval="1m")
    d.logs(2644, "Tunnel status transitions", "Transitions since process start (first poll emits none); observed timestamp is part of the retry-stable event key.",
        f'{LOG} | event_name="cloudflare.tunnel.status_change"')

    return tab(TAB_PLATFORM, [
        row("At a glance", [(pid, 3, 4) for pid, *_ in glance]),
        row("Workers", [(501, 16, 9), (2611, 8, 9)]),
        row("Workers invocation health", [(2630, 12, 8), (2631, 12, 8), (2632, 12, 8), (2633, 12, 8), (2634, 12, 8), (2635, 12, 8)]),
        row("Certificates (current snapshot)", [(2640, 12, 10), (2645, 12, 10)]),
        row("Tunnels", [(2641, 24, 8), (2642, 12, 8), (2643, 12, 8), (2644, 24, 8)]),
        row("D1 and KV", [(601, 9, 7), (603, 9, 7), (604, 6, 7), (605, 12, 7), (606, 6, 7), (607, 6, 7)]),
        row("R2", [(705, 12, 8), (701, 12, 8), (706, 8, 8), (707, 8, 8), (703, 8, 8), (708, 24, 5)]),
        row("Durable Objects", [(801, 10, 7), (804, 10, 7), (803, 4, 7), (2651, 8, 7), (2652, 8, 7), (2653, 8, 7)]),
        row("D1 rows and query batches", [(2654, 8, 7), (2655, 8, 7), (2656, 8, 7)]),
        row("Queue lag, retries and actions", [(2657, 8, 7), (2658, 8, 7), (2659, 8, 7)]),
        row("Logpush failures", [(2650, 24, 8)]),
        row("Health checks (Pro-only opt-in)", [(2660, 24, 8), (2661, 12, 7), (2662, 12, 7), (2663, 12, 7), (2664, 12, 7)]),
        row("Queues", [(905, 12, 7), (901, 12, 7), (902, 12, 7), (903, 12, 7)]),
        row("Turnstile, Logpush and Email", [(502, 8, 7), (503, 8, 7), (2621, 8, 7)]),
    ])


def collector_tab(d: Dashboard) -> dict:
    never = [mapping_range(1e9, 1e12, "never succeeded", RED)]
    d.stat(2701, "Collectors reporting", "Distinct collectors with a last-success timestamp in the latest sample.",
        [prom(f'count(max by (cf2otel_collector) (cf2otel_scrape_last_success_timestamp_seconds{{{S}}}))', instant=True)])
    d.stat(2702, "Stale collectors", "Collectors whose last successful poll is older than 15 minutes, or that never succeeded. Zero is healthy.",
        [prom(f'count((time() - max by (cf2otel_collector) (cf2otel_scrape_last_success_timestamp_seconds{{{S}}})) > 900) or vector(0)', instant=True)],
        thresholds=ZERO_GOOD)
    d.stat(403, "Export failures", "Export failures over the selected range; zero is healthy if the exporter reports the series.",
        [prom(f'sum(increase(cf2otel_export_errors_total{{{S}}}[$__range])) or vector(0)', instant=True)], thresholds=ZERO_GOOD, decimals=0)
    d.stat(407, "API retries", "Cloudflare API retry count over the selected range. Retries absorb rate limits and transient 5xx; a steady climb means Cloudflare is pushing back.",
        [prom(f'sum(increase(cf2otel_api_retries_total{{{S}}}[$__range])) or vector(0)', instant=True)], decimals=0, thresholds=ZERO_GOOD_WARN)
    d.stat(2703, "Dropped window commits", "Windows whose commit failed permanently after repeated payload rejection. Each one is data that was read but never exported.",
        [prom(f'sum(increase(cf2otel_window_commit_failures_total{{{S},outcome="dropped"}}[$__range])) or vector(0)', instant=True)],
        thresholds=ZERO_GOOD, decimals=0)
    d.stat(2704, "Running version", "cf2otel build version and commit reported by each running instance.",
        [prom(f'count by (cf2otel_build_version, commit) (label_replace(cf2otel_build_info_ratio{{{S}}}, "commit", "$1", "cf2otel_build_commit", "(.{{7}}).*"))',
              "{{cf2otel_build_version}} @ {{commit}}", instant=True)], text_mode="name")

    d.table(401, "Collector last-success age", "Seconds since each collector last succeeded, with checkpoint age, poll outcomes and p95 poll duration over the "
        "selected range. An absent series needs investigation too. Ages over 15 minutes are stale (the collector-stale alert fires); "
        "checkpoint ages over 12 hours risk the Access log's one-day reach.",
        [table_q(f'max by (cf2otel_collector) (time() - cf2otel_scrape_last_success_timestamp_seconds{{{S}}})', "A"),
         table_q(f'max by (cf2otel_collector) (cf2otel_checkpoint_age_seconds{{{S}}})', "B"),
         table_q(f'sum by (cf2otel_collector) (increase(cf2otel_scrape_success_total{{{S}}}[$__range]))', "C"),
         table_q(f'sum by (cf2otel_collector) (increase(cf2otel_scrape_errors_total{{{S}}}[$__range]))', "D"),
         table_q(f'histogram_quantile(0.95, sum by (cf2otel_collector, le) (rate(cf2otel_scrape_duration_seconds_bucket{{{S}}}[$__range])))', "E")],
        columns={"cf2otel_collector": "Collector", "Value #A": "Last success", "Value #B": "Checkpoint age", "Value #C": "Successful polls",
                 "Value #D": "Failed polls", "Value #E": "Poll p95"},
        order=["cf2otel_collector", "Value #A", "Value #B", "Value #C", "Value #D", "Value #E"], sort_by="Last success",
        overrides=[by_name("Last success", unit="s", decimals=0, mappings=never, thresholds=steps((GREEN, None), (YELLOW, 600), (RED, 900)),
                           custom={"cellOptions": {"type": "color-background", "mode": "basic"}, "align": "auto"}),
                   by_name("Checkpoint age", unit="s", decimals=0, thresholds=steps((GREEN, None), (YELLOW, 3600), (RED, 43200)),
                           custom={"cellOptions": {"type": "color-text"}, "align": "auto"}),
                   by_name("Successful polls", decimals=0),
                   by_name("Failed polls", decimals=0, thresholds=steps(("text", None), (ORANGE, 1)), custom={"cellOptions": {"type": "color-text"}, "align": "auto"}),
                   by_name("Poll p95", unit="s", decimals=2)])
    d.state_timeline(2711, "Collector freshness", "Seconds since each collector's last successful poll, over time. Green is fresh, yellow is late, "
        "red is stale (over 15 minutes, the alert threshold).",
        [prom(f'max by (cf2otel_collector) (time() - cf2otel_scrape_last_success_timestamp_seconds{{{S}}})', "{{cf2otel_collector}}")],
        unit="s", thresholds=steps((GREEN, None), (YELLOW, 600), (RED, 900)), mappings=never)

    d.ts(402, "Collector errors", "Failed polls by collector. Only collectors with at least one failure appear; empty means no failures.",
        [prom(f'sum by (cf2otel_collector) (increase(cf2otel_scrape_errors_total{{{S}}}[$__interval])) > 0', "{{cf2otel_collector}}")],
        unit="short", bars=True, stack=True, decimals=0, interval="15m", no_value="No failed polls")
    d.bargauge(404, "Checkpoint age", "Age of each collector checkpoint; compare with its configured interval. Over 12 hours on access.logins "
        "approaches the Access REST log's roughly one-day reach.",
        [prom(f'sort_desc(max by (cf2otel_collector) (cf2otel_checkpoint_age_seconds{{{S}}}))', "{{cf2otel_collector}}", instant=True)],
        unit="s", thresholds=steps((GREEN, None), (YELLOW, 3600), (RED, 43200)), mode="lcd")
    d.ts(405, "Window retention gaps", "Retention-gap seconds skipped over the selected range, by collector; any nonzero value is permanent data loss for that window. "
        "Empty is healthy.",
        [prom(f'sum by (cf2otel_collector) (increase(cf2otel_window_gap_seconds_total{{{S}}}[$__interval])) > 0', "{{cf2otel_collector}}")],
        unit="s", bars=True, stack=True, interval="15m", no_value="No retention gaps")
    d.ts(406, "Window commit failures", "Failed window commits over the selected range, by collector and outcome; a dropped outcome is permanent data loss for that window. "
        "Empty is healthy.",
        [prom(f'sum by (cf2otel_collector, outcome) (increase(cf2otel_window_commit_failures_total{{{S}}}[$__interval])) > 0', "{{cf2otel_collector}} {{outcome}}")],
        unit="short", bars=True, stack=True, decimals=0, interval="15m", overrides=[by_regexp(".* dropped", color=fixed(RED))], no_value="No commit failures")
    d.ts(2712, "Catch-up windows", "Extra bounded windows committed in one scheduler tick while a collector catches up after an outage or at start-up.",
        [prom(f'sum by (cf2otel_collector) (increase(cf2otel_window_catchup_windows_total{{{S}}}[$__interval])) > 0', "{{cf2otel_collector}}")],
        unit="short", bars=True, stack=True, decimals=0, interval="15m", no_value="No catch-up in range")
    d.bargauge(2713, "Slowest collectors (poll p95)", "95th percentile poll duration over the selected range for the 10 slowest collectors. "
        "The histogram has finite buckets through 120 s; these are bucket-bound estimates, not exact timings.",
        [prom(f'sort_desc(topk(10, histogram_quantile(0.95, sum by (cf2otel_collector, le) (rate(cf2otel_scrape_duration_seconds_bucket{{{S}}}[$__range])))))',
              "{{cf2otel_collector}}", instant=True)], unit="s", decimals=1, thresholds=steps((GREEN, None), (YELLOW, 10), (RED, 30)), mode="lcd")

    d.ts(408, "API requests by status class", "Cloudflare API request rate by status class. 0xx means the request got no HTTP response.",
        [prom(f'sum by (cf2otel_status_class) (rate(cf2otel_api_requests_total{{{S}}}[$__rate_interval]))', "{{cf2otel_status_class}}")],
        unit="reqps", stack=True, overrides=color_overrides(STATUS_COLORS))
    d.ts(2723, "API logical envelope errors", "Unsuccessful Cloudflare envelopes returned with HTTP 200, separate from actual HTTP attempt status. Permission code 9109 is classified as 4xx.",
        [prom(f'sum by (cf2otel_status_class) (rate(cf2otel_api_envelope_errors_total{{{S}}}[$__rate_interval]))', "{{cf2otel_status_class}}")], unit="ops")
    d.ts(2724, "Metric cardinality overflows", "Datapoints exceeding the SDK cardinality limit, by instrument. The default limit is 10000; config zero removes the limit.",
        [prom(f'sum by (cf2otel_instrument) (rate(cf2otel_metric_cardinality_overflows_total{{{S}}}[$__rate_interval]))', "{{cf2otel_instrument}}")], unit="ops")
    d.heatmap(2721, "API latency distribution", "Cloudflare API attempt duration from the cf2otel.api.duration histogram.",
        [prom(f'sum by (le) (increase(cf2otel_api_duration_seconds_bucket{{{S}}}[$__rate_interval]))', "{{le}}", fmt="heatmap")])
    d.ts(2722, "API latency percentiles", "Median, p95 and p99 Cloudflare API attempt duration.",
        [prom(f'histogram_quantile({q}, sum by (le) (rate(cf2otel_api_duration_seconds_bucket{{{S}}}[$__rate_interval])))', f"p{int(q * 100)}", ref=r)
         for r, q in (("A", 0.5), ("B", 0.95), ("C", 0.99))], unit="s")

    d.ts(2731, "OTLP exports by signal", "Export batches per signal (logs, metrics, traces): successes above zero, failures below.",
        [prom(f'sum by (cf2otel_export_signal) (rate(cf2otel_export_success_total{{{S}}}[$__rate_interval]))', "{{cf2otel_export_signal}} ok", ref="A"),
         prom(f'sum by (cf2otel_export_signal) (rate(cf2otel_export_errors_total{{{S}}}[$__rate_interval]))', "{{cf2otel_export_signal}} failed", ref="B")],
        unit="ops", min_zero=False, overrides=[by_regexp(".* failed", custom={"transform": "negative-Y"}, color=fixed(RED))])

    d.logs(2741, "Retention-gap events", "One event per skipped window with the collector, the window start and the retention floor. Empty is healthy.",
        f'{LOG} | event_name="cf2otel.window.gap"')

    return tab(TAB_COLLECTOR, [
        row("Status", [(2701, 4, 4), (2702, 4, 4), (403, 4, 4), (407, 4, 4), (2703, 4, 4), (2704, 4, 4), (401, 24, 12), (2711, 24, 18)]),
        row("Polling and windows", [(402, 12, 8), (404, 12, 16), (405, 6, 8), (406, 6, 8), (2712, 12, 8), (2713, 12, 8)]),
        row("Cloudflare API and export", [(408, 8, 8), (2721, 8, 8), (2722, 8, 8), (2731, 24, 7), (2723, 12, 8), (2724, 12, 8)]),
        row("Retention-gap logs", [(2741, 24, 8)], collapse=True),
    ])


# --------------------------------------------------------------------------------------------
# Variables and assembly
# --------------------------------------------------------------------------------------------

def datasource(name: str, label: str, plugin: str, default: str) -> dict:
    return {"kind": "DatasourceVariable", "spec": {"name": name, "label": label, "pluginId": plugin,
        "current": {"text": default, "value": default}, "options": [], "multi": False, "includeAll": False,
        "allowCustomValue": True, "hide": "dontHide", "refresh": "onDashboardLoad", "regex": "", "skipUrlSync": False}}


def query_var(name: str, label: str, query: str, *, regex: str = "", qry_type: int = 1, description: str = "") -> dict:
    return {"kind": "QueryVariable", "spec": {"name": name, "label": label, "description": description,
        "hide": "dontHide", "skipUrlSync": False, "multi": True, "includeAll": True, "allValue": ".*",
        "allowCustomValue": True, "current": {"text": ["All"], "value": ["$__all"]}, "options": [],
        "refresh": "onTimeRangeChanged", "sort": "alphabeticalCaseInsensitiveAsc", "regex": regex, "regexApplyTo": "value",
        "definition": query, "query": {"kind": "DataQuery", "version": "v0", "group": "prometheus",
            "datasource": {"name": PROM}, "spec": {"query": query, "qryType": qry_type, "refId": f"{name}-variable"}}}}


ZONE_QUERY = ("query_result(count by (zone) ("
    f'label_replace(cloudflare_http_requests_total{{{S}}}, "zone", "$1", "cloudflare_http_zone", "(.+)") or '
    f'label_replace(cloudflare_dns_queries_total{{{S}}}, "zone", "$1", "cloudflare_dns_zone", "(.+)") or '
    f'label_replace(cloudflare_firewall_events_total{{{S}}}, "zone", "$1", "cloudflare_firewall_zone", "(.+)")))')


def variables() -> list[dict]:
    return [
        datasource("ds_prometheus", "Prometheus", "prometheus", "grafanacloud-prom"),
        datasource("ds_loki", "Loki", "loki", "grafanacloud-logs"),
        datasource("ds_tempo", "Tempo", "tempo", "grafanacloud-traces"),
        query_var("zone", "Zone", ZONE_QUERY, regex='/zone="(?<value>[^"]+)"/', qry_type=3,
                  description="Cloudflare zones seen by the HTTP, DNS or firewall collectors."),
        query_var("host", "Host", f'label_values(cloudflare_http_requests_total{{{S},cloudflare_http_zone=~"$zone"}}, cloudflare_http_host)'),
        query_var("gateway", "AI gateway", f'label_values(cloudflare_ai_gateway_requests_total{{{S}}}, cloudflare_ai_gateway_gateway_name)'),
        query_var("script", "Worker", f'label_values(cloudflare_workers_requests_total{{{S}}}, cloudflare_workers_script_name)'),
        query_var("bucket", "R2 bucket", f'label_values(cloudflare_r2_requests_total{{{S}}}, cloudflare_r2_bucket_name)'),
        query_var("site_tag", "Web Analytics site", f'label_values(cloudflare_rum_page_views_total{{{S}}}, cloudflare_rum_site_tag)'),
    ]


def render() -> dict:
    d = Dashboard()
    tabs = [overview(d), http_tab(d), security_tab(d), dns_tab(d), access_tab(d), ai_tab(d), web_tab(d), platform_tab(d), collector_tab(d)]
    placed = {item["spec"]["element"]["name"] for t in tabs for r in t["spec"]["layout"]["spec"]["rows"]
              for item in r["spec"]["layout"]["spec"]["items"]}
    if placed != set(d.elements):
        raise ValueError(f"layout/element mismatch: unplaced={sorted(set(d.elements) - placed)} missing={sorted(placed - set(d.elements))}")
    docs = "https://github.com/rknightion/cf2otel/blob/main/docs"
    return {"apiVersion": "dashboard.grafana.app/v2", "kind": "Dashboard", "metadata": {"name": "cf2otel"},
        "spec": {"title": "Cloudflare to OpenTelemetry",
        "description": "Cloudflare estate overview (HTTP, security, DNS, Access, AI Gateway, Web Analytics, Workers and platform) plus cf2otel collector health. "
                       "Generated by grafana/build_dashboard.py.",
        "tags": ["cloudflare", "cf2otel", "generated"], "annotations": audit_annotations(), "preload": False, "cursorSync": "Crosshair",
        "links": [
            {"title": "Signal reference", "type": "link", "icon": "doc", "tooltip": "Every metric, log event and attribute cf2otel emits",
             "url": f"{docs}/signals.md", "tags": [], "asDropdown": False, "targetBlank": True, "includeVars": False, "keepTime": False},
            {"title": "Troubleshooting", "type": "link", "icon": "question", "tooltip": "What to do when a collector is stale or failing",
             "url": f"{docs}/troubleshooting.md", "tags": [], "asDropdown": False, "targetBlank": True, "includeVars": False, "keepTime": False},
        ],
        "timeSettings": {"from": "now-24h", "to": "now", "autoRefresh": "5m", "autoRefreshIntervals": ["1m", "5m", "15m", "1h"],
            "timezone": "browser", "hideTimepicker": False, "fiscalYearStartMonth": 0},
        "variables": variables(), "elements": d.elements, "layout": {"kind": "TabsLayout", "spec": {"tabs": tabs}}}}


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
