"""Public generator contract for the account audit annotation layer.

The fixture follows Grafana's v2-to-v1 annotation adapter (legacyOptions are
spread at the root) and Loki's annotationQuery (templates read row.labels).
It is local contract evidence, not an execution of Grafana or Loki.
"""
import json
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


if __name__ == "__main__":
    unittest.main()
