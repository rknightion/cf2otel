# cf2otel Helm chart

The chart runs one outbound-only cf2otel poller. Its checkpoint directory is a
persistent volume. A second replica would race on checkpoints, so the chart
always renders one StatefulSet replica. It creates only the headless Service
required to govern the StatefulSet, with no exposed port or host networking.

Create a Kubernetes Secret named `cf2otel` with keys `cloudflare-api-token` and
`otlp-token`, or change the `existingSecret` values to reference an existing
Secret. Supply account and Grafana instance identifiers through a private
values file. Keep credentials out of values files: the chart only reads them
through Secret key references and never renders a Secret.

```yaml
config:
  cloudflare:
    account_id: "<account-id>"
  otlp:
    endpoint: "https://<otlp-gateway>/otlp"
    grafana_cloud:
      instance_id: "<instance-id>"
```

Install with:

```sh
helm install cf2otel oci://ghcr.io/rknightion/charts/cf2otel -f private-values.yaml
```

The application health listener stays on Pod loopback;
the readiness and liveness probes invoke the binary's `-healthcheck` flag inside
the container. The pod uses non-root UID 65532, a read-only root filesystem,
runtime-default seccomp, no Linux capabilities and no mounted Kubernetes API
token. Keep the checkpoint PVC when replacing the release if continuity matters.

<!-- x-release-please-start-version -->
Chart and app version: 0.5.1.
<!-- x-release-please-end -->
