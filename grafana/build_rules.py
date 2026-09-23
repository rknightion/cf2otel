#!/usr/bin/env python3
"""Generate cf2otel Grafana-managed alert rules."""
from __future__ import annotations

import argparse
import json
from pathlib import Path

from build_dashboard import render

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "alerts" / "grafana-managed"
FOLDER = "REPLACE_WITH_FOLDER_UID"
PROM = "grafanacloud-prom"

RULES = [
    ("cf2otel-collector-stale", "cf2otel collector is stale", "Collector last-success timestamp is older than 15 minutes.", 401,
     'time() - max by (cf2otel_collector) (cf2otel_scrape_last_success_timestamp{service_name="cf2otel"})', 900, "Alerting"),
    ("cf2otel-export-failure", "cf2otel export is failing", "OTLP export failures occurred in the last 15 minutes.", 403,
     'sum(increase(cf2otel_export_errors_total{service_name="cf2otel"}[15m])) or on() (0 * sum(increase(cf2otel_export_success_total{service_name="cf2otel"}[15m])))', 0, "Alerting"),
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


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    for rule in RULES:
        path = OUT / f"{rule[0]}.json"
        content = json.dumps(resource(*rule), indent=2, sort_keys=True) + "\n"
        if args.check:
            if not path.exists() or path.read_text(encoding="utf-8") != content:
                raise SystemExit(f"generated rule drift: {path.relative_to(ROOT)}")
        else:
            OUT.mkdir(parents=True, exist_ok=True)
            path.write_text(content, encoding="utf-8")


if __name__ == "__main__":
    main()
