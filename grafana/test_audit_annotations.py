"""Public generator contract for the account audit annotation layer.

The fixture follows Grafana's v2-to-v1 annotation adapter (legacyOptions are
spread at the root) and Loki's annotationQuery (templates read row.labels).
It is local contract evidence, not an execution of Grafana or Loki.
"""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


def without_resource_and_dex_additions(dashboard):
    """Earlier feature-preservation tests compare their pre-DASH-3 surface.

    These two additive rows have separate query/behavior contracts below. Retain
    all prior rows and panels in the whole-dashboard equality checks.
    """
    platform = next(t for t in dashboard["spec"]["layout"]["spec"]["tabs"]
                    if t["spec"]["title"] == "Workers and platform")
    rows = platform["spec"]["layout"]["spec"]["rows"]
    for row in list(rows):
        if row["spec"]["title"] in ("Resolved platform resources and R2 actions",
                                    "DEX test results (opt-in provider averages)"):
            for item in row["spec"]["layout"]["spec"]["items"]:
                dashboard["spec"]["elements"].pop(item["spec"]["element"]["name"])
            rows.remove(row)


def without_bot_and_lb_additions(dashboard):
    """Exclude only DASH-4's explicit additive rows, not any earlier object."""
    additions = {
        "Security": ("Advertised bot score dimensions", {"panel-2117", "panel-2118"}),
        "Workers and platform": ("Load balancer provider flags (partial opt-in)", {"panel-2690"}),
    }
    for tab in dashboard["spec"]["layout"]["spec"]["tabs"]:
        addition = additions.get(tab["spec"]["title"])
        if addition is None:
            continue
        title, panels = addition
        rows = tab["spec"]["layout"]["spec"]["rows"]
        for row in list(rows):
            if row["spec"]["title"] == title:
                actual = {item["spec"]["element"]["name"]
                          for item in row["spec"]["layout"]["spec"]["items"]}
                if actual != panels:
                    raise AssertionError("unexpected objects in DASH-4 additive row")
                for panel in panels:
                    dashboard["spec"]["elements"].pop(panel)
                rows.remove(row)


class AuditAnnotationsTest(unittest.TestCase):
    def test_generated_layer_reaches_loki_with_audit_context(self):
        # Exercise the generator's public CLI without rewriting the tracked output.
        with tempfile.TemporaryDirectory() as directory:
            script = Path(directory) / "grafana" / "build_dashboard.py"
            script.parent.mkdir()
            script.write_bytes((ROOT / "grafana/build_dashboard.py").read_bytes())
            subprocess.run([sys.executable, str(script)], check=True, timeout=30)
            dashboard = json.loads((script.parent.parent / "dashboards/cf2otel.json").read_text())
        layers = [a for a in dashboard["spec"]["annotations"]
                  if a["spec"]["name"] == "Account audit changes"]
        self.assertEqual(len(layers), 1, "account audit needs one independent annotation layer")
        layer = layers[0]
        self.assertEqual(layer["kind"], "AnnotationQuery")
        spec = layer["spec"]
        self.assertIs(spec["enable"], False)
        self.assertIs(spec["hide"], False)
        query = spec["query"]
        self.assertEqual(query["kind"], "DataQuery")
        self.assertEqual(query["group"], "loki")
        self.assertEqual(query["version"], "v0")
        loki_variable = next(v for v in dashboard["spec"]["variables"]
                             if v["spec"]["name"] == "ds_loki")
        self.assertEqual(query["datasource"]["name"], "${" + loki_variable["spec"]["name"] + "}")
        self.assertEqual(query["spec"], {})
        # Grafana's reverse adapter restores these datasource properties at the root.
        runtime = {k: spec[k] for k in ("enable", "hide", "name", "iconColor")}
        runtime.update(spec["legacyOptions"])
        self.assertEqual(runtime["expr"], '{service_name="cf2otel"} | event_name="cloudflare.audit.event"')
        self.assertIs(runtime["instant"], False)
        self.assertEqual(runtime["maxLines"], 100)
        labels = {
            # Identity fixtures stay opaque; the runtime attribute key is unchanged.
            "cloudflare_audit_actor_email": "fixture-actor",
            "cloudflare_audit_actor_token_name": "fixture credential",
            "cloudflare_audit_actor_type": "api",
            "cloudflare_audit_action_description": "Change policy",
            "cloudflare_audit_action_type": "update",
            "cloudflare_audit_resource_product": "access",
            "cloudflare_audit_resource_type": "policy",
            "cloudflare_audit_resource_id": "fixture-resource",
        }
        template_fields = set(re.findall(r"{{\s*(\w+)\s*}}", runtime["titleFormat"] + runtime["textFormat"]))
        self.assertTrue(set(labels).issubset(template_fields), "actor, action and resource metadata must survive")
        for email in (labels["cloudflare_audit_actor_email"], ""):
            row_labels = {**labels, "cloudflare_audit_actor_email": email}
            # The supplied Loki source uses renderLegendFormat on row.labels.
            rendered = [re.sub(r"{{\s*(\w+)\s*}}", lambda m: row_labels[m[1]], runtime[key])
                        for key in ("titleFormat", "textFormat")]
            self.assertIn(row_labels["cloudflare_audit_actor_token_name"], rendered[0])
            self.assertIn(row_labels["cloudflare_audit_action_description"], rendered[0])
            for value in row_labels.values():
                if value:
                    self.assertIn(value, " ".join(rendered))


class LandedMetricPanelsTest(unittest.TestCase):
    def generated_targets(self):
        with tempfile.TemporaryDirectory() as directory:
            script = Path(directory) / "grafana" / "build_dashboard.py"
            script.parent.mkdir()
            script.write_bytes((ROOT / "grafana/build_dashboard.py").read_bytes())
            subprocess.run([sys.executable, str(script)], check=True, timeout=30)
            dashboard = json.loads((script.parent.parent / "dashboards/cf2otel.json").read_text())
        queries = [q["spec"]["query"] for p in dashboard["spec"]["elements"].values()
                   for q in p["spec"]["data"]["spec"]["queries"]]
        return [q["spec"] for q in queries if q["group"] == "prometheus"]

    def test_workers_ai_counters_keep_independent_instances_and_range(self):
        targets = self.generated_targets()
        # Names follow semconv metadata plus Prometheus otlptranslator's escaping,
        # monotonic-counter suffix and unit rules (s -> seconds, {token} omitted).
        metrics = ("cloudflare_workers_ai_inferences_total",
                   "cloudflare_workers_ai_input_tokens_total",
                   "cloudflare_workers_ai_output_tokens_total",
                   "cloudflare_workers_ai_inference_time_seconds_total")
        for metric in metrics:
            matches = [t for t in targets if metric + "{" in t["expr"]]
            self.assertTrue(matches, f"missing Workers AI counter query: {metric}")
            rates = [t for t in matches if not t["instant"]]
            self.assertTrue(rates, f"missing Workers AI rate: {metric}")
            for target in matches:
                operation = "increase" if target["instant"] else "rate"
                window = "$__range" if target["instant"] else "$__rate_interval"
                self.assertEqual(target["expr"],
                                 f'sum by (instance) ({operation}({metric}'
                                 f'{{service_name="cf2otel"}}[{window}]))',
                                 "different exporter instances must not be summed or absent sources zero-filled")
                self.assertIn("{{instance}}", target["legendFormat"])
            if metric != metrics[-1]:
                self.assertTrue(any(t["instant"] for t in matches),
                                f"missing selected-range total: {metric}")

    def test_workers_ai_generated_queries_handle_resets_replicas_and_absence(self):
        targets = [t for t in self.generated_targets()
                   if "cloudflare_workers_ai_" in t["expr"]]
        self.assertTrue(targets, "Workers AI queries must exist before evaluating them")
        tests = []
        for target in targets:
            metric = re.search(r"(cloudflare_workers_ai_\w+)\{", target["expr"])[1]
            expression = target["expr"].replace("$__range", "5m").replace("$__rate_interval", "5m")
            # Instance A resets, B does not; C is absent (source off/unavailable).
            # Both rate and increase must adjust each counter before aggregation.
            labels = lambda instance: f'{{instance="{instance}"}}'
            values = (225, 300) if target["instant"] else (0.75, 1)
            tests.append({"expr": expression, "eval_time": "5m", "exp_samples": [
                {"labels": labels(instance), "value": value}
                for instance, value in zip(("fixture-a", "fixture-b"), values)]})
            tests.append({"expr": expression, "eval_time": "15m", "exp_samples": []})
        metrics = sorted({re.search(r"(cloudflare_workers_ai_\w+)\{", t["expr"])[1] for t in targets})
        series = [{"series": f'{metric}{{service_name="cf2otel",instance="{instance}"}}',
                   "values": values}
                  for metric in metrics
                  for instance, values in (("fixture-a", "0 60 0 60 120 180"),
                                           ("fixture-b", "0 60 120 180 240 300"))]
        fixture = {"rule_files": [], "evaluation_interval": "1m", "tests": [{
            "interval": "1m", "input_series": series, "promql_expr_test": tests}]}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "workers-ai.json"
            path.write_text(json.dumps(fixture))
            result = subprocess.run(["promtool", "test", "rules", str(path)],
                                    capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_zone_selection_gauges_reach_generated_dashboard(self):
        targets = self.generated_targets()
        for metric in ("cf2otel_zones_discovered", "cf2otel_zones_filtered",
                       "cf2otel_zones_processed", "cf2otel_zones_skipped"):
            matches = [t for t in targets if metric + "{" in t["expr"]]
            self.assertTrue(matches, f"missing zone selection gauge query: {metric}")
            for target in matches:
                self.assertNotRegex(target["expr"], r"(?:rate|increase)\(")
                self.assertIn("instance", target["expr"])
                self.assertIn("cf2otel_collector", target["expr"])
                if metric.endswith(("filtered", "skipped")):
                    self.assertIn("cf2otel_zone_reason", target["expr"])

    def test_error_classes_reach_generated_dashboard(self):
        targets = self.generated_targets()
        self.assertTrue(any("cf2otel_scrape_errors_total{" in t["expr"] and
                            "cf2otel_error_class" in t["expr"] for t in targets),
                        "missing bounded collector error-class breakdown")

    def test_firewall_enrichment_reaches_generated_metric_table(self):
        targets = self.generated_targets()
        self.assertTrue(any("cloudflare_firewall_events_total{" in t["expr"] and
                            all(label in t["expr"] for label in (
                                "cloudflare_firewall_rule_id", "cloudflare_firewall_rule_description",
                                "cloudflare_firewall_host", "cloudflare_firewall_client_country"))
                            and t["instant"] and t["format"] == "table" for t in targets),
                        "missing enriched firewall metric table; logs are not a substitute")


class ResourceAndDEXPanelsTest(unittest.TestCase):
    # Public OTLP names/units are frozen in semconv metadata; suffixes verified
    # with the pinned Prometheus otlptranslator, not inferred from Grafana units.
    resources = {
        "cloudflare_d1_database_name": (
            "cloudflare_d1_read_queries_total", "cloudflare_d1_write_queries_total",
            "cloudflare_d1_queries_total", "cloudflare_d1_storage_max_database_bytes"),
        "cloudflare_kv_namespace_name": (
            "cloudflare_kv_requests_total", "cloudflare_kv_storage_max_namespace_bytes",
            "cloudflare_kv_storage_max_namespace_keys"),
        "cloudflare_durableobjects_namespace_name": (
            "cloudflare_durableobjects_requests_total", "cloudflare_durableobjects_subrequests_total",
            "cloudflare_durableobjects_subrequests_request_body_bytes_total",
            "cloudflare_durableobjects_sql_storage_max_namespace_bytes"),
        "cloudflare_queues_queue_name": (
            "cloudflare_queues_message_operations_total", "cloudflare_queues_message_billable_operations_total",
            "cloudflare_queues_backlog_max_queue_avg_messages",
            "cloudflare_queues_delayed_backlog_max_queue_avg_messages",
            "cloudflare_queues_backlog_max_queue_avg_bytes",
            "cloudflare_queues_consumer_max_queue_avg_concurrency"),
        "cloudflare_r2_action_type": ("cloudflare_r2_requests_total",),
    }
    dex = {
        "cloudflare_dex_http_fetch_time_milliseconds": "ms",
        "cloudflare_dex_traceroute_rtt_milliseconds": "ms",
        "cloudflare_dex_traceroute_hops": "suffix: hops",
        "cloudflare_dex_packet_loss_percent": "percent",
        "cloudflare_dex_availability_percent": "percent",
    }

    def panels(self):
        result, dashboard = HTTPDimensionsTest().generate()
        self.assertEqual(result.returncode, 0, result.stderr)
        return dashboard["spec"]["elements"].values()

    def targets(self):
        return [q["spec"]["query"]["spec"] for panel in self.panels()
                for q in panel["spec"]["data"]["spec"]["queries"]
                if q["spec"]["query"]["group"] == "prometheus"]

    def test_resolved_resources_keep_raw_instance_and_source_reductions(self):
        targets = self.targets()
        for label, metrics in self.resources.items():
            for metric in metrics:
                with self.subTest(metric=metric):
                    matches = [t for t in targets if metric + "{" in t["expr"] and
                               "{{" + label + "}}" in t["legendFormat"] and
                               "{{instance}}" in t["legendFormat"]]
                    self.assertTrue(matches, f"missing resolved resource/action query: {metric}")
                    selector = 'service_name="cf2otel"'
                    if label == "cloudflare_r2_action_type":
                        selector += ',cloudflare_r2_bucket_name=~"$bucket"'
                    series = f"{metric}{{{selector}}}"
                    expected = f"rate({series}[$__rate_interval])" if metric.endswith("_total") else series
                    for target in matches:
                        self.assertEqual(target["expr"], expected,
                                         "retain account/instance/resource and source MAX; never combine replicas")
                        self.assertFalse(target["instant"])
                        self.assertIn("{{cloudflare_account_name}}", target["legendFormat"])
                        if label == "cloudflare_r2_action_type":
                            self.assertIn("{{cloudflare_r2_bucket_name}}", target["legendFormat"])

    def test_dex_provider_averages_keep_kind_name_units_and_absence(self):
        panels = list(self.panels())
        for metric, unit in self.dex.items():
            matches = [p["spec"] for p in panels if any(
                q["spec"]["query"]["group"] == "prometheus" and
                metric + "{" in q["spec"]["query"]["spec"]["expr"]
                for q in p["spec"]["data"]["spec"]["queries"])]
            self.assertTrue(matches, f"missing provider-average DEX panel: {metric}")
            for panel in matches:
                target = panel["data"]["spec"]["queries"][0]["spec"]["query"]["spec"]
                self.assertEqual(target["expr"], f'{metric}{{service_name="cf2otel"}}',
                                 "no pooled average, gauge rate, unit conversion or unavailable zero-fill")
                self.assertEqual(set(re.findall(r"{{(\w+)}}", target["legendFormat"])),
                                 {"instance", "cloudflare_dex_test_name", "cloudflare_dex_test_kind"})
                defaults = panel["vizConfig"]["spec"]["fieldConfig"]["defaults"]
                self.assertEqual(defaults["unit"], unit)
                self.assertEqual(defaults["noValue"], "Unavailable")
                self.assertIn("provider requested-interval averages", panel["description"])
                self.assertIn("arithmetic mean", panel["description"])
                self.assertIn("fixture-only", panel["description"])
                self.assertIn("disabled by default", panel["description"])

    def test_generated_queries_preserve_remainders_replicas_and_real_zero(self):
        targets = self.targets()
        expressions = []
        for label, metric in (("cloudflare_d1_database_name", "cloudflare_d1_queries_total"),
                              ("cloudflare_queues_queue_name", "cloudflare_queues_backlog_max_queue_avg_messages"),
                              ("cloudflare_dex_test_name", "cloudflare_dex_availability_percent")):
            matches = [t for t in targets if metric + "{" in t["expr"] and
                       "{{" + label + "}}" in t["legendFormat"] and "{{instance}}" in t["legendFormat"]]
            self.assertTrue(matches, f"missing query for behavioral evaluation: {metric}")
            expressions.append((label, metric, matches[0]["expr"].replace("$__rate_interval", "5m")))
        series, checks = [], []
        for label, metric, expr in expressions:
            samples = []
            for instance, name, values, expected in (
                    ("fixture-a", "Named", "0 60 0 60 120 180", 0.75),
                    ("fixture-b", "Named", "0 60 120 180 240 300", 1),
                    ("fixture-a", "other", "0 0 0 0 0 0", 0)):
                attributes = f'instance="{instance}",{label}="{name}",service_name="cf2otel"'
                if metric.startswith("cloudflare_dex_"):
                    attributes += ',cloudflare_dex_test_kind="http"'
                labels = attributes
                if not metric.endswith("_total"):
                    labels += f',__name__="{metric}"'
                    expected = 40 if instance == "fixture-a" and name == "Named" else 90 if instance == "fixture-b" else 0
                    values = " ".join([str(expected)] * 6)
                series.append({"series": f'{metric}{{{attributes}}}', "values": values})
                samples.append({"labels": "{" + labels + "}", "value": expected})
            checks += [{"expr": expr, "eval_time": "5m", "exp_samples": samples},
                       {"expr": expr, "eval_time": "15m", "exp_samples": []}]
        fixture = {"rule_files": [], "evaluation_interval": "1m", "tests": [{
            "interval": "1m", "input_series": series, "promql_expr_test": checks}]}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "resource-dex.json"
            path.write_text(json.dumps(fixture))
            result = subprocess.run(["promtool", "test", "rules", str(path)],
                                    capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


class AccessSeatsTest(unittest.TestCase):
    def generate(self, interval=None):
        with tempfile.TemporaryDirectory() as directory:
            script = Path(directory) / "grafana" / "build_dashboard.py"
            script.parent.mkdir()
            script.write_bytes((ROOT / "grafana/build_dashboard.py").read_bytes())
            env = dict(os.environ)
            env.pop("GRAFANA_ACCESS_SEATS_INTERVAL_SECONDS", None)
            if interval is not None:
                env["GRAFANA_ACCESS_SEATS_INTERVAL_SECONDS"] = interval
            result = subprocess.run([sys.executable, str(script)], env=env,
                                    capture_output=True, text=True, timeout=30)
            output = script.parent.parent / "dashboards/cf2otel.json"
            return result, json.loads(output.read_text()) if output.exists() else None

    def test_latest_independent_seats_have_per_instance_freshness(self):
        for setting, threshold in ((None, 2700), ("60", 180)):
            with self.subTest(setting=setting):
                result, dashboard = self.generate(setting)
                self.assertEqual(result.returncode, 0, result.stderr)
                panels = [p["spec"] for p in dashboard["spec"]["elements"].values()
                          if p["spec"]["title"] == "Access and Gateway seats"]
                self.assertEqual(len(panels), 1, "independent seats panel must be generated")
                panel = panels[0]
                queries = panel["data"]["spec"]["queries"]
                self.assertEqual(len(queries), 2)
                for query, seat_type, legend in zip(queries, ("access", "gateway"), ("Access", "Gateway")):
                    data = query["spec"]["query"]
                    self.assertEqual(data["group"], "prometheus")
                    self.assertEqual(data["datasource"]["name"], "${ds_prometheus}")
                    target = data["spec"]
                    self.assertIs(target["instant"], True)
                    self.assertIs(target["range"], False)
                    self.assertEqual(target["legendFormat"], legend)
                    expected = ('max by (cloudflare_access_seat_type) ('
                                'cloudflare_access_seats_ratio{service_name="cf2otel",'
                                f'cloudflare_access_seat_type="{seat_type}"}} and on (instance) '
                                '(time() - max by (instance) ('
                                'cf2otel_scrape_last_success_timestamp_seconds{service_name="cf2otel",'
                                f'cf2otel_collector="access.seats"}}) < {threshold}))')
                    self.assertEqual(target["expr"], expected,
                                     "filter stale instances before max; retain genuine zero; never fill absent data")
                defaults = panel["vizConfig"]["spec"]["fieldConfig"]["defaults"]
                self.assertEqual(defaults["unit"], "none")
                self.assertEqual(defaults["noValue"], "No data")
                self.assertEqual(defaults["mappings"], [])

    def test_invalid_generator_intervals_fail_without_output(self):
        for setting in ("0", "-1", "1.5", "invalid", "", "１２"):
            with self.subTest(setting=setting):
                result, dashboard = self.generate(setting)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("GRAFANA_ACCESS_SEATS_INTERVAL_SECONDS must be a positive integer in seconds", result.stderr)
                self.assertIsNone(dashboard)


class WARPFleetTest(unittest.TestCase):
    def generate(self, interval=None, window=None):
        with tempfile.TemporaryDirectory() as directory:
            script = Path(directory) / "grafana" / "build_dashboard.py"
            script.parent.mkdir()
            script.write_bytes((ROOT / "grafana/build_dashboard.py").read_bytes())
            env = dict(os.environ)
            for key in ("GRAFANA_WARP_INTERVAL_SECONDS", "GRAFANA_WARP_LAST_SEEN_WINDOW_SECONDS",
                        "GRAFANA_ACCESS_SEATS_INTERVAL_SECONDS", "GRAFANA_CERTS_PACKS_INTERVAL_SECONDS"):
                env.pop(key, None)
            if interval is not None:
                env["GRAFANA_WARP_INTERVAL_SECONDS"] = interval
            if window is not None:
                env["GRAFANA_WARP_LAST_SEEN_WINDOW_SECONDS"] = window
            result = subprocess.run([sys.executable, str(script)], env=env,
                                    capture_output=True, text=True, timeout=30)
            output = script.parent.parent / "dashboards/cf2otel.json"
            return result, json.loads(output.read_text()) if output.exists() else None

    def test_raw_recent_counts_preserve_source_dimensions_and_instance(self):
        columns = {
            "cloudflare_account_name": "Account", "instance": "Exporter instance",
            "cloudflare_warp_status": "Network status", "cloudflare_warp_platform": "Platform",
            "cloudflare_warp_client_version": "Client version", "cloudflare_warp_mode": "WARP mode",
            "cloudflare_warp_colo": "Colo", "cloudflare_warp_remainder": "Remainder", "Value": "Devices",
        }
        for interval, window, threshold, seconds in ((None, None, 900, 900), ("600", "1800", 1800, 1800)):
            with self.subTest(interval=interval, window=window):
                result, dashboard = self.generate(interval, window)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("panel-2325", dashboard["spec"]["elements"],
                              "landed WARP devices need a source-preserving table")
                panel = dashboard["spec"]["elements"]["panel-2325"]["spec"]
                self.assertEqual(panel["title"], "WARP recently seen devices")
                self.assertEqual(panel["vizConfig"]["group"], "table")
                queries = panel["data"]["spec"]["queries"]
                self.assertEqual(len(queries), 1)
                query = queries[0]["spec"]["query"]
                self.assertEqual(query["group"], "prometheus")
                self.assertEqual(query["datasource"]["name"], "${ds_prometheus}")
                target = query["spec"]
                self.assertIs(target["instant"], True)
                self.assertIs(target["range"], False)
                self.assertEqual(target["format"], "table")
                self.assertEqual(target["expr"],
                                 '(cloudflare_warp_devices_ratio{service_name="cf2otel"} and on (instance) '
                                 '(time() - max by (instance) ('
                                 'cf2otel_scrape_last_success_timestamp_seconds{service_name="cf2otel",'
                                 f'cf2otel_collector="warp.fleet"}}) < {threshold}))')
                transforms = panel["data"]["spec"]["transformations"]
                self.assertEqual(len(transforms), 1, "no reduction or aggregation of source rows")
                self.assertEqual(transforms[0]["kind"], "organize")
                options = transforms[0]["spec"]["options"]
                self.assertEqual(options["renameByName"], columns)
                self.assertEqual(options["indexByName"], {key: i for i, key in enumerate(columns)})
                self.assertFalse(set(columns) & {k for k, hidden in options["excludeByName"].items() if hidden})
                self.assertTrue(options["excludeByName"]["Time"])
                self.assertTrue(options["excludeByName"]["__name__"])
                # Model organize's public field selection on a returned table frame. Literal
                # 'other' is distinct from overflow; neither strings nor counts are remapped.
                source = {key: f"fixture-{key}" for key in columns}
                source.update(cloudflare_warp_status="unknown", cloudflare_warp_platform="other",
                              cloudflare_warp_remainder="false", Value=7, Time=123, __name__="fixture-metric")
                def visible(frame):
                    return {options["renameByName"].get(k, k): v for k, v in frame.items()
                            if not options["excludeByName"].get(k, False)}
                self.assertEqual(visible(source), {columns[k]: source[k] for k in columns})
                remainder = {**source, "cloudflare_warp_remainder": "true", "Value": 2}
                self.assertEqual(visible(remainder)["Remainder"], "true")
                self.assertEqual(visible(remainder)["Devices"], 2)
                defaults = panel["vizConfig"]["spec"]["fieldConfig"]["defaults"]
                self.assertEqual(defaults["unit"], "short")
                self.assertEqual(defaults["decimals"], 0)
                overrides = panel["vizConfig"]["spec"]["fieldConfig"]["overrides"]
                self.assertIn({"matcher": {"id": "byName", "options": "Devices"},
                               "properties": [{"id": "min", "value": 0}]}, overrides)
                self.assertFalse(panel["vizConfig"]["spec"]["options"]["footer"]["show"])
                self.assertIn(f"{seconds} seconds", panel["description"])
                self.assertIn("GRAFANA_WARP_INTERVAL_SECONDS", panel["description"])
                self.assertIn("GRAFANA_WARP_LAST_SEEN_WINDOW_SECONDS", panel["description"])

    def test_appended_access_row_preserves_existing_dashboard_semantics(self):
        result, dashboard = self.generate()
        self.assertEqual(result.returncode, 0, result.stderr)
        access = next(t for t in dashboard["spec"]["layout"]["spec"]["tabs"]
                      if t["spec"]["title"] == "Access and Zero Trust")
        rows = access["spec"]["layout"]["spec"]["rows"]
        self.assertEqual(rows[-1]["spec"]["title"], "WARP fleet")
        items = rows[-1]["spec"]["layout"]["spec"]["items"]
        self.assertEqual(len(items), 1)
        self.assertEqual(items[0]["spec"]["element"]["name"], "panel-2325")
        self.assertEqual(items[0]["spec"]["width"], 24)
        self.assertEqual(items[0]["spec"]["x"], 0)
        rows.pop()
        dashboard["spec"]["elements"].pop("panel-2325")
        # Compare all existing semantic content to the parent committed asset, not
        # incidental JSON ordering or a hardcoded panel count. This also survives landing.
        baseline = subprocess.run(["git", "show", "HEAD:dashboards/cf2otel.json"], cwd=ROOT,
                                  check=True, capture_output=True, text=True, timeout=30)
        original = json.loads(baseline.stdout)
        if "panel-2325" in original["spec"]["elements"]:
            original["spec"]["elements"].pop("panel-2325")
            old_access = next(t for t in original["spec"]["layout"]["spec"]["tabs"]
                              if t["spec"]["title"] == "Access and Zero Trust")
            old_access["spec"]["layout"]["spec"]["rows"].pop()
        # The later HTTP batch has its own whole-dashboard preservation check below.
        # Exclude only that explicitly appended feature from this earlier WARP comparison.
        for asset in (dashboard, original):
            if "panel-2065" in asset["spec"]["elements"]:
                http = next(t for t in asset["spec"]["layout"]["spec"]["tabs"]
                            if t["spec"]["title"] == "HTTP and cache")
                http["spec"]["layout"]["spec"]["rows"].pop()
                for pid in (2065, 2066, 2067):
                    asset["spec"]["elements"].pop(f"panel-{pid}")
        for asset in (dashboard, original):
            without_resource_and_dex_additions(asset)
            without_bot_and_lb_additions(asset)
        self.assertEqual(dashboard, original, "all previous panels, layouts and dashboard settings must survive")

    def test_invalid_deployment_settings_fail_without_output(self):
        cases = (("0", None), ("-1", None), ("+1", None), ("1.5", None), ("１２", None),
                 (None, "0"), (None, "3601"), (None, "１２"))
        for interval, window in cases:
            with self.subTest(interval=interval, window=window):
                result, dashboard = self.generate(interval, window)
                self.assertNotEqual(result.returncode, 0)
                key = "GRAFANA_WARP_INTERVAL_SECONDS" if interval is not None else "GRAFANA_WARP_LAST_SEEN_WINDOW_SECONDS"
                self.assertIn(key + " must be", result.stderr)
                self.assertIsNone(dashboard)


class HTTPDimensionsTest(unittest.TestCase):
    def generate(self, interval=None):
        with tempfile.TemporaryDirectory() as directory:
            script = Path(directory) / "grafana" / "build_dashboard.py"
            script.parent.mkdir()
            script.write_bytes((ROOT / "grafana/build_dashboard.py").read_bytes())
            env = {k: v for k, v in os.environ.items() if not k.startswith("GRAFANA_")}
            if interval is not None:
                env["GRAFANA_HTTP_HIGH_CARDINALITY_INTERVAL_SECONDS"] = interval
            result = subprocess.run([sys.executable, str(script)], env=env,
                                    capture_output=True, text=True, timeout=30)
            output = script.parent.parent / "dashboards/cf2otel.json"
            return result, json.loads(output.read_text()) if output.exists() else None

    def test_source_counter_rates_preserve_every_tuple_and_instance(self):
        dimensions = (
            (2065, "HTTP requests by colo", "cloudflare_http_requests_by_colo_total",
             ("cloudflare_http_colo",)),
            (2066, "HTTP requests by ASN", "cloudflare_http_requests_by_asn_total",
             ("cloudflare_http_client_asn", "cloudflare_http_client_asn_description")),
            (2067, "HTTP errors by configured route", "cloudflare_http_errors_by_route_total",
             ("cloudflare_http_route_name", "cloudflare_http_status_class")),
        )
        for setting, threshold in ((None, 900), ("600", 1800)):
            with self.subTest(setting=setting):
                result, dashboard = self.generate(setting)
                self.assertEqual(result.returncode, 0, result.stderr)
                for pid, title, family, fields in dimensions:
                    key = f"panel-{pid}"
                    self.assertIn(key, dashboard["spec"]["elements"], "landed HTTP dimensions need panels")
                    panel = dashboard["spec"]["elements"][key]["spec"]
                    self.assertEqual(panel["title"], title)
                    self.assertEqual(panel["vizConfig"]["group"], "timeseries")
                    queries = panel["data"]["spec"]["queries"]
                    self.assertEqual(len(queries), 1)
                    query = queries[0]["spec"]["query"]
                    self.assertEqual(query["kind"], "DataQuery")
                    self.assertEqual(query["version"], "v0")
                    self.assertEqual(query["group"], "prometheus")
                    self.assertEqual(query["datasource"]["name"], "${ds_prometheus}")
                    target = query["spec"]
                    self.assertIs(target["instant"], False)
                    self.assertIs(target["range"], True)
                    self.assertEqual(target["expr"],
                                     f'rate({family}{{service_name="cf2otel",cloudflare_http_zone=~"$zone"}}[$__rate_interval]) '
                                     'and on (instance) (time() - max by (instance) ('
                                     'cf2otel_scrape_last_success_timestamp_seconds{service_name="cf2otel",'
                                     f'cf2otel_collector="httpreq.metrics"}}) < {threshold})')
                    expected_fields = {"cloudflare_http_zone", "instance", "cloudflare_http_breakdown_remainder", *fields}
                    self.assertEqual(set(re.findall(r"{{(\w+)}}", target["legendFormat"])), expected_fields)
                    defaults = panel["vizConfig"]["spec"]["fieldConfig"]["defaults"]
                    self.assertEqual(defaults["unit"], "reqps")
                    self.assertEqual(defaults["custom"]["stacking"]["mode"], "none")
                    self.assertIs(defaults["custom"]["spanNulls"], False)
                    self.assertEqual(panel["data"]["spec"]["transformations"], [])

    def test_appended_http_row_preserves_entire_prior_dashboard(self):
        result, dashboard = self.generate()
        self.assertEqual(result.returncode, 0, result.stderr)
        http = next(t for t in dashboard["spec"]["layout"]["spec"]["tabs"]
                    if t["spec"]["title"] == "HTTP and cache")
        rows = http["spec"]["layout"]["spec"]["rows"]
        self.assertEqual(rows[-1]["spec"]["title"], "Opt-in HTTP dimensions")
        items = rows[-1]["spec"]["layout"]["spec"]["items"]
        self.assertEqual([(i["spec"]["element"]["name"], i["spec"]["width"], i["spec"]["x"]) for i in items],
                         [("panel-2065", 8, 0), ("panel-2066", 8, 8), ("panel-2067", 8, 16)])
        rows.pop()
        for pid in (2065, 2066, 2067):
            dashboard["spec"]["elements"].pop(f"panel-{pid}")
        baseline = subprocess.run(["git", "show", "HEAD:dashboards/cf2otel.json"], cwd=ROOT,
                                  check=True, capture_output=True, text=True, timeout=30)
        original = json.loads(baseline.stdout)
        if "panel-2065" in original["spec"]["elements"]:
            old_http = next(t for t in original["spec"]["layout"]["spec"]["tabs"]
                            if t["spec"]["title"] == "HTTP and cache")
            old_http["spec"]["layout"]["spec"]["rows"].pop()
            for pid in (2065, 2066, 2067):
                original["spec"]["elements"].pop(f"panel-{pid}")
        for asset in (dashboard, original):
            without_resource_and_dex_additions(asset)
            without_bot_and_lb_additions(asset)
        self.assertEqual(dashboard, original, "only the new HTTP panels and appended row may change")

    def test_invalid_static_intervals_fail_without_output(self):
        for setting in ("0", "１２"):
            with self.subTest(setting=setting):
                result, dashboard = self.generate(setting)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("GRAFANA_HTTP_HIGH_CARDINALITY_INTERVAL_SECONDS must be a positive integer in seconds", result.stderr)
                self.assertIsNone(dashboard)


class BotAndPoolPanelsTest(unittest.TestCase):
    def dashboard(self):
        return HTTPDimensionsTest().generate()[1]

    def target(self, pid):
        dashboard = self.dashboard()
        self.assertIn(f"panel-{pid}", dashboard["spec"]["elements"],
                      "landed bot dimensions and partial pool health need source panels")
        panel = dashboard["spec"]["elements"][f"panel-{pid}"]["spec"]
        return panel["data"]["spec"]["queries"][0]["spec"]["query"]["spec"]

    def evaluate(self, series, tests):
        fixture = {"rule_files": [], "evaluation_interval": "1m", "tests": [{
            "interval": "1m", "input_series": series, "promql_expr_test": tests}]}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "bot-pool.json"
            path.write_text(json.dumps(fixture))
            result = subprocess.run(["promtool", "test", "rules", str(path)],
                                    capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_bot_queries_keep_reset_instances_numeric_bins_and_source_remainder(self):
        metric = "cloudflare_firewall_events_total"
        # Same tuple on independent pollers, one resetting. Source-only enrichment
        # and base-only counts must not fabricate numeric bins or zeroes.
        tuples = [("fixture-a", "250-255", "other", "0 60 0 60 120 180", 0.75),
                  ("fixture-b", "250-255", "other", "0 60 120 180 240 300", 1),
                  ("fixture-a", "0-9", "fixture-source", "0 0 0 0 0 0", 0),
                  ("fixture-a", "", "fixture-source", "0 30 60 90 120 150", 0.5),
                  ("fixture-a", "", "", "0 60 120 180 240 300", 1)]
        series, samples = [], []
        for instance, bucket, source, values, value in tuples:
            labels = {"service_name": "cf2otel", "instance": instance,
                      "cloudflare_firewall_zone": "fixture-zone"}
            if bucket:
                labels["cloudflare_firewall_bot_score_bucket"] = bucket
            if source:
                labels["cloudflare_firewall_bot_score_source"] = source
            encoded = ",".join(f'{key}="{val}"' for key, val in sorted(labels.items()))
            series.append({"series": metric + "{" + encoded + "}", "values": values})
            samples.append({"labels": "{" + encoded + "}", "value": value})
        for pid, field in ((2117, "cloudflare_firewall_bot_score_bucket"),
                           (2118, "cloudflare_firewall_bot_score_source")):
            target = self.target(pid)
            expression = target["expr"].replace("$zone", "fixture-zone").replace("$__rate_interval", "5m")
            expected = [s for s in samples if field + "=" in s["labels"]]
            self.evaluate(series, [{"expr": expression, "eval_time": "5m", "exp_samples": expected},
                                   {"expr": expression, "eval_time": "15m", "exp_samples": []}])

    def test_pool_query_keeps_provider_zero_one_other_instances_and_stale_absence(self):
        target = self.target(2690)
        metric = "cloudflare_loadbalancers_pool_health_ratio"
        series, samples = [], []
        for instance, pool, value in (("fixture-a", "fixture-pool", 0),
                                      ("fixture-b", "fixture-pool", 1),
                                      ("fixture-a", "other", 0)):
            labels = (f'instance="{instance}",service_name="cf2otel",'
                      f'cloudflare_loadbalancers_pool_name="{pool}"')
            series.append({"series": metric + "{" + labels + "}",
                           "values": f"{value} {value} stale"})
            samples.append({"labels": metric + "{" + labels + "}", "value": value})
        self.evaluate(series, [{"expr": target["expr"], "eval_time": "1m", "exp_samples": samples},
                               {"expr": target["expr"], "eval_time": "2m", "exp_samples": []}])

    def test_only_explicit_bot_and_pool_additions_change_prior_objects(self):
        dashboard = self.dashboard()
        for pid in (2117, 2118, 2690):
            self.assertIn(f"panel-{pid}", dashboard["spec"]["elements"])
        baseline = subprocess.run(["git", "show", "HEAD:dashboards/cf2otel.json"], cwd=ROOT,
                                  check=True, capture_output=True, text=True, timeout=30)
        original = json.loads(baseline.stdout)
        for asset in (dashboard, original):
            without_bot_and_lb_additions(asset)
        self.assertEqual(dashboard, original, "all old objects and layout must remain unchanged")


if __name__ == "__main__":
    unittest.main()
