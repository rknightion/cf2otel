import contextlib
import copy
import io
import json
import os
import tempfile
import unittest
from http.client import IncompleteRead
from pathlib import Path
from unittest.mock import patch
from urllib.error import HTTPError, URLError
from urllib.parse import parse_qs, urlsplit

import verify_deployment as verifier


REVISION = "a" * 40
TIME = 1700000000.125
URL = "https://example.test/api/datasources/proxy/uid/example-prom/api/v1/query"
TOKEN = "test-only-token"
INSTANCE = "example-instance"


def observation():
    return {
        "status": "success",
        "data": {
            "resultType": "vector",
            "result": [{
                "metric": {verifier.BUILD_LABEL: REVISION, "instance": INSTANCE},
                "value": [TIME, "12.4"],
            }],
        },
    }


class VerifyDeployment(unittest.TestCase):
    def setUp(self):
        scratch = self.enterContext(tempfile.TemporaryDirectory())
        self.token_file = Path(scratch) / "read-token"
        self.token_file.write_text(TOKEN + "\n", encoding="ascii")
        self.enterContext(
            patch.dict(os.environ, {
                "CF2OTEL_DEPLOY_QUERY_URL": URL,
                "CF2OTEL_DEPLOY_TOKEN_FILE": str(self.token_file),
            }, clear=True)
        )
        self.enterContext(patch.object(verifier.time, "time", return_value=TIME))
        self.opener = self.enterContext(patch.object(verifier, "build_opener"))
        self.response = self.opener.return_value.open.return_value.__enter__.return_value
        self.log = io.StringIO()
        self.enterContext(contextlib.redirect_stdout(self.log))
        self.enterContext(contextlib.redirect_stderr(self.log))

    def reply(self, payload):
        self.response.read.return_value = json.dumps(payload).encode()

    def test_fresh_exact_revision_uses_only_provisioned_get(self):
        self.reply(observation())
        self.assertEqual(verifier.main([REVISION]), 0)
        request = self.opener.return_value.open.call_args.args[0]
        parsed = urlsplit(request.full_url)
        self.assertEqual(request.get_method(), "GET")
        self.assertEqual(request.get_header("Authorization"), f"Bearer {TOKEN}")
        self.assertEqual(request.get_header("Cache-control"), "no-cache")
        self.assertEqual(parsed.path, urlsplit(URL).path)
        self.assertEqual(parse_qs(parsed.query), {
            "query": ["time() - timestamp(cf2otel_build_info_ratio)"],
            "time": [str(TIME)],
        })
        handler = self.opener.call_args.args[0]
        self.assertIsNone(handler.redirect_request(None, None, 302, "", {}, "https://other.test"))
        self.assertNotIn(TOKEN, self.log.getvalue())
        self.assertNotIn(URL, self.log.getvalue())

    def test_malformed_missing_multiple_stale_and_wrong_are_unread(self):
        valid = observation()["data"]["result"][0]
        peer = copy.deepcopy(valid)
        peer["metric"]["instance"] = "other-example-instance"
        cases = (
            ("missing", []),
            ("duplicate", [valid, valid]),
            ("two instances of one revision", [valid, peer]),
            ("identity missing", [{"metric": {verifier.BUILD_LABEL: REVISION}, "value": valid["value"]}]),
            ("identity empty", [{"metric": {verifier.BUILD_LABEL: REVISION, "instance": ""}, "value": valid["value"]}]),
            ("wrong", [{"metric": {verifier.BUILD_LABEL: "b" * 40, "instance": INSTANCE}, "value": valid["value"]}]),
            ("malformed label", [{"metric": "invalid", "value": valid["value"]}]),
            ("malformed value", [{"metric": valid["metric"], "value": [TIME]}]),
            ("stale sample", [{"metric": valid["metric"], "value": [TIME, "120.1"]}]),
            ("stale response", [{"metric": valid["metric"], "value": [TIME - 3, "12"]}]),
            ("future sample", [{"metric": valid["metric"], "value": [TIME, "-1"]}]),
            ("non-finite sample", [{"metric": valid["metric"], "value": [TIME, "NaN"]}]),
        )
        for name, rows in cases:
            with self.subTest(name=name):
                payload = observation()
                payload["data"]["result"] = copy.deepcopy(rows)
                self.reply(payload)
                self.assertEqual(verifier.main([REVISION]), 2)

    def test_provider_errors_are_unread_and_redacted(self):
        for failure in (HTTPError(URL, 401, TOKEN, {}, None), URLError(TOKEN), TimeoutError(TOKEN)):
            with self.subTest(failure=type(failure).__name__):
                self.opener.return_value.open.side_effect = failure
                self.assertEqual(verifier.main([REVISION]), 2)
        self.assertNotIn(TOKEN, self.log.getvalue())
        self.assertNotIn(URL, self.log.getvalue())

    def test_missing_credential_and_invalid_route_do_not_contact_provider(self):
        self.token_file.unlink()
        self.assertEqual(verifier.main([REVISION]), 2)
        self.token_file.write_text(TOKEN, encoding="ascii")
        with patch.dict(os.environ, {"CF2OTEL_DEPLOY_QUERY_URL": "https://example.test/"}):
            self.assertEqual(verifier.main([REVISION]), 2)
        self.opener.assert_not_called()

    def test_invalid_revision_is_an_invocation_error(self):
        self.assertEqual(verifier.main(["a" * 12]), 1)
        self.opener.assert_not_called()

    def test_malformed_or_oversized_provider_body_is_unread(self):
        for raw in (b"{broken", b" " * (verifier.MAX_RESPONSE_BYTES + 1)):
            self.response.read.return_value = raw
            self.assertEqual(verifier.main([REVISION]), 2)

    def test_partial_provider_body_is_unread_and_redacted(self):
        self.response.read.side_effect = IncompleteRead(TOKEN.encode())
        self.assertEqual(verifier.main([REVISION]), 2)
        self.assertNotIn(TOKEN, self.log.getvalue())


if __name__ == "__main__":
    unittest.main()
