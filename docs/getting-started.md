# Getting started

cf2otel needs a Cloudflare API token with read access to the surfaces you enable, an account ID, and an OTLP destination. The Cloudflare token and OTLP credential must be environment variables. Put non-secret settings in a YAML file based on [`config.example.yaml`](https://github.com/rknightion/cf2otel/blob/main/config.example.yaml).

## Run the container

Create a writable state directory owned by the container user (UID 65532), and a configuration file readable by that user. Supply the two secrets at runtime:

```sh
docker run --rm --read-only --user 65532:65532 \
  -v /path/to/config.yaml:/etc/cf2otel/config.yaml:ro \
  -v /path/to/state:/var/lib/cf2otel \
  -e CF2OTEL_CLOUDFLARE__API_TOKEN \
  -e CF2OTEL_OTLP__GRAFANA_CLOUD__TOKEN \
  ghcr.io/rknightion/cf2otel:<release-tag> \
  -config /etc/cf2otel/config.yaml
```

Set `cloudflare.account_id`, `otlp.endpoint` and `otlp.grafana_cloud.instance_id` in YAML, or use the matching environment variables. The OTLP endpoint is the root URL of the destination's OTLP gateway. The default protocol is HTTP; gRPC is also supported.

The example enables the registered collectors, with `aigateway.metrics` disabled because GraphQL Groups ingestion lag is unbounded. AI Gateway metrics are supplied by the REST log collector. Start with the default `http.scope: access_protected` so request events are restricted to hosts found in Access applications. See [Configuration](configuration.md) before using the broader `hosts` or `all` scopes.

## Check output

The state directory holds checkpoints. Keep it on persistent storage across restarts. The loopback health endpoint defaults to `127.0.0.1:9464`; the image's healthcheck uses the local CLI probe. In Loki, OTLP log attributes are structured metadata. A query starts with the service label, for example:

```logql
{service_name="cf2otel"} | event_name="cloudflare.access.login"
```

Check [Signals](signals.md) for metric names and the GenAI trace mapping. A successful poll with no new events may emit no event log; inspect scrape and checkpoint metrics before treating that as a failure.
