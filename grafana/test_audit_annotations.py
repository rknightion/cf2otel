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


if __name__ == "__main__":
    unittest.main()
