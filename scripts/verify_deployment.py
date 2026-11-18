"""Read CF2Otel's deployed build revision through its provisioned Grafana proxy."""

import json
import math
import os
import re
import sys
import time
from http.client import HTTPException
from pathlib import Path
from urllib.parse import urlencode, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener


REVISION = re.compile(r"[0-9a-f]{40}")
QUERY_PATH = re.compile(r"/api/datasources/proxy/uid/[A-Za-z0-9_-]+/api/v1/query")
BUILD_LABEL = "cf2otel_build_commit"
QUERY = "time() - timestamp(cf2otel_build_info_ratio)"
MAX_SAMPLE_AGE = 120
REQUEST_TIMEOUT = 12
MAX_RESPONSE_BYTES = 65536


class NoRedirect(HTTPRedirectHandler):
    """Keep the read credential scoped to the configured origin."""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def proxy_config():
    url = os.environ.get("CF2OTEL_DEPLOY_QUERY_URL", "")
    parsed = urlsplit(url)
    if (
        parsed.scheme != "https"
        or not parsed.hostname
        or parsed.port not in (None, 443)
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
        or not QUERY_PATH.fullmatch(parsed.path)
    ):
        raise ValueError("invalid provisioned query endpoint")

    token_path = Path(os.environ.get("CF2OTEL_DEPLOY_TOKEN_FILE", ""))
    if not token_path.is_absolute():
        raise ValueError("expected an absolute token file path")
    with token_path.open(encoding="ascii") as handle:
        token = handle.read(4097).strip()
    if not re.fullmatch(r"[!-~]{1,4096}", token):
        raise ValueError("invalid provisioned token")
    return url, token


def read_observation(url, token, requested_at):
    params = urlencode({"query": QUERY, "time": requested_at})
    request = Request(
        f"{url}?{params}",
        headers={
            "Accept": "application/json",
            "Authorization": f"Bearer {token}",
            "Cache-Control": "no-cache",
        },
        method="GET",
    )
    with build_opener(NoRedirect()).open(request, timeout=REQUEST_TIMEOUT) as response:
        raw = response.read(MAX_RESPONSE_BYTES + 1)
    if len(raw) > MAX_RESPONSE_BYTES:
        raise ValueError("oversized observation")
    return json.loads(raw)


def matches(payload, revision, requested_at):
    if not isinstance(payload, dict) or payload.get("status") != "success":
        return False
    data = payload.get("data")
    if not isinstance(data, dict) or data.get("resultType") != "vector":
        return False
    rows = data.get("result")
    if not isinstance(rows, list) or len(rows) != 1 or not isinstance(rows[0], dict):
        return False
    row = rows[0]
    labels = row.get("metric")
    if not isinstance(labels, dict) or labels.get(BUILD_LABEL) != revision:
        return False
    instance = labels.get("instance")
    if not isinstance(instance, str) or not instance.strip():
        return False
    value = row.get("value")
    if not isinstance(value, list) or len(value) != 2:
        return False
    evaluated_at, sample_age = value
    if type(evaluated_at) not in (int, float) or not isinstance(sample_age, str):
        return False
    try:
        age = float(sample_age)
        return (
            math.isfinite(evaluated_at)
            and abs(requested_at - evaluated_at) <= 1
            and 0 <= age <= MAX_SAMPLE_AGE
        )
    except (ValueError, OverflowError):
        return False


def main(args):
    if len(args) != 1 or not REVISION.fullmatch(args[0]):
        print("Deployment verification requires a full lowercase revision", file=sys.stderr)
        return 1

    requested_at = time.time()
    try:
        url, token = proxy_config()
        observed = read_observation(url, token, requested_at)
    except (HTTPException, OSError, ValueError):
        print("Deployment observation is unavailable", file=sys.stderr)
        return 2

    if not matches(observed, args[0], requested_at):
        print("Deployment observation does not establish the requested revision", file=sys.stderr)
        return 2

    print("Fresh deployment observation matches the requested revision")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
