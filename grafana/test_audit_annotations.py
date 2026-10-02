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
        self.assertEqual(dashboard, original, "only the new HTTP panels and appended row may change")

    def test_invalid_static_intervals_fail_without_output(self):
        for setting in ("0", "１２"):
            with self.subTest(setting=setting):
                result, dashboard = self.generate(setting)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("GRAFANA_HTTP_HIGH_CARDINALITY_INTERVAL_SECONDS must be a positive integer in seconds", result.stderr)
                self.assertIsNone(dashboard)


if __name__ == "__main__":
    unittest.main()
