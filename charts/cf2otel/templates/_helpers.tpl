{{- define "cf2otel.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "cf2otel.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "cf2otel.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "cf2otel.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "cf2otel.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cf2otel.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "cf2otel.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "cf2otel.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "cf2otel.config" -}}
{{- $config := deepCopy .Values.config -}}
{{- if hasKey $config.cloudflare "api_token" -}}
{{- fail "config.cloudflare.api_token is secret; use existingSecret" -}}
{{- end -}}
{{- if hasKey $config.otlp.grafana_cloud "token" -}}
{{- fail "config.otlp.grafana_cloud.token is secret; use existingSecret" -}}
{{- end -}}
{{- if and (hasKey $config.otlp "headers") (not (empty $config.otlp.headers)) -}}
{{- fail "config.otlp.headers may contain secrets; use an existing Secret" -}}
{{- end -}}
{{- if ne $config.state.dir "/var/lib/cf2otel" -}}
{{- fail "config.state.dir must be /var/lib/cf2otel to use the checkpoint PVC" -}}
{{- end -}}
{{- $_ := set $config.state "dir" "/var/lib/cf2otel" -}}
{{- $config | toYaml -}}
{{- end -}}
