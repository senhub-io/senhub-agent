{{/* Chart name, truncated to a DNS label. */}}
{{- define "senhub-agent.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Fully qualified release name. */}}
{{- define "senhub-agent.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "senhub-agent.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "senhub-agent.selectorLabels" -}}
app.kubernetes.io/name: {{ include "senhub-agent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "senhub-agent.labels" -}}
helm.sh/chart: {{ include "senhub-agent.chart" . }}
{{ include "senhub-agent.selectorLabels" . }}
app.kubernetes.io/version: {{ include "senhub-agent.imageTag" . | quote }}
app.kubernetes.io/component: agent
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "senhub-agent.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "senhub-agent.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "senhub-agent.imageTag" -}}
{{- default .Chart.AppVersion .Values.image.tag }}
{{- end }}

{{- define "senhub-agent.image" -}}
{{- $repo := .Values.image.repository }}
{{- if not $repo }}
{{- if eq .Values.edition "oss" }}
{{- $repo = "ghcr.io/senhub-io/senhub-agent-oss" }}
{{- else if eq .Values.edition "full" }}
{{- $repo = "ghcr.io/senhub-io/senhub-agent" }}
{{- else }}
{{- fail (printf "edition must be full or oss, not %q" .Values.edition) }}
{{- end }}
{{- end }}
{{- printf "%s:%s" $repo (include "senhub-agent.imageTag" .) }}
{{- end }}

{{- define "senhub-agent.hostname" -}}
{{- default (include "senhub-agent.fullname" .) .Values.hostname | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "senhub-agent.identitySecretName" -}}
{{- default (printf "%s-identity" (include "senhub-agent.fullname" .)) .Values.identity.existingSecret }}
{{- end }}

{{- define "senhub-agent.envSecretName" -}}
{{- printf "%s-env" (include "senhub-agent.fullname" .) }}
{{- end }}

{{/*
Whether the pod needs the service account token: only the kubernetes
probe talks to the API server.
*/}}
{{- define "senhub-agent.automountToken" -}}
{{- if or .Values.rbac.kubernetesProbe.enabled .Values.serviceAccount.automountToken }}true{{ else }}false{{ end }}
{{- end }}

{{/*
A fragment key becomes a file name: keep it to what a file name and a
ConfigMap key both accept.
*/}}
{{- define "senhub-agent.checkFragmentKey" -}}
{{- if not (regexMatch "^[a-z0-9][a-z0-9_-]*$" .) }}
{{- fail (printf "config fragment key %q: use lowercase letters, digits, - and _ only" .) }}
{{- end }}
{{- end }}

{{/*
The SENHUB_* variables the image entrypoint reads, as a YAML list. A value
is set only when given: a boolean false is a value (SENHUB_OTLP_TLS=false),
an empty string is not.
*/}}
{{- define "senhub-agent.entrypointEnv" -}}
{{- $e := .Values.env }}
{{- $tags := list }}
{{- range $k, $v := $e.tags }}
{{- $tags = append $tags (printf "%s=%s" $k (toString $v)) }}
{{- end }}
{{- $pairs := list
  (list "SENHUB_OTLP_ENDPOINT" $e.otlpEndpoint)
  (list "SENHUB_OTLP_PROTOCOL" $e.otlpProtocol)
  (list "SENHUB_OTLP_TLS" $e.otlpTLS)
  (list "SENHUB_ENTITIES" $e.entities)
  (list "SENHUB_TAGS" (join "," $tags))
  (list "SENHUB_ZABBIX_SERVER" $e.zabbixServer)
  (list "SENHUB_ZABBIX_HOST_METADATA" $e.zabbixHostMetadata)
  (list "SENHUB_AZURE_APP" $e.azure.app)
  (list "SENHUB_AZURE_TENANT_ID" $e.azure.tenantId)
  (list "SENHUB_AZURE_CLIENT_ID" $e.azure.clientId)
  (list "SENHUB_AZURE_SUBSCRIPTION_ID" $e.azure.subscriptionId)
  (list "SENHUB_AZURE_RESOURCE_GROUP" $e.azure.resourceGroup)
  (list "TZ" $e.timezone) }}
{{- $out := list }}
{{- range $pairs }}
{{- $v := toString (index . 1) }}
{{- if and (ne $v "") (ne $v "<nil>") }}
{{- $out = append $out (dict "name" (index . 0) "value" $v) }}
{{- end }}
{{- end }}
{{- toYaml $out }}
{{- end }}

{{/* agent.yaml when the operator provides one. */}}
{{- define "senhub-agent.agentYAML" -}}
{{- $cfg := deepCopy .Values.config.agent }}
{{- $agent := default (dict) (get $cfg "agent") }}
{{- if not (get $agent "key") }}
{{- $_ := set $agent "key" "${env:SENHUB_AGENT_KEY}" }}
{{- end }}
{{- $_ := set $cfg "agent" $agent }}
{{- if not (hasKey $cfg "config_version") }}
{{- $_ := set $cfg "config_version" 3 }}
{{- end }}
{{- toYaml $cfg }}
{{- end }}

{{/*
Deployment or DaemonSet. Empty `kind` picks a DaemonSet when the node is
monitored (one agent per node, each with its own identity) and a
Deployment otherwise.
*/}}
{{- define "senhub-agent.kind" -}}
{{- $k := default "" .Values.kind }}
{{- if eq $k "" }}
{{- if .Values.hostMonitoring.enabled }}DaemonSet{{ else }}Deployment{{ end }}
{{- else if or (eq $k "Deployment") (eq $k "DaemonSet") }}{{ $k }}
{{- else }}
{{- fail (printf "kind must be empty, Deployment or DaemonSet, not %q" $k) }}
{{- end }}
{{- end }}

{{/*
Refuses what makes no sense with one agent per node: a cluster-wide
setting would run on every node, and a shared identity would merge every
node into one agent.
*/}}
{{- define "senhub-agent.checkDaemonSet" -}}
{{- if eq (include "senhub-agent.kind" .) "DaemonSet" }}
{{- if .Values.kubernetesProbe.enabled }}
{{- fail "kubernetesProbe.enabled with a DaemonSet would collect the whole cluster once per node. Install a second release with kind=Deployment and hostMonitoring.enabled=false for the cluster probe" }}
{{- end }}
{{- if .Values.config.agent }}
{{- fail "config.agent carries one agent key, and a DaemonSet runs one agent per node: leave it empty (use env and config.probes/strategies), or use kind=Deployment" }}
{{- end }}
{{- if .Values.serviceMonitor.enabled }}
{{- fail "serviceMonitor.enabled needs one shared agent key and one Service; a DaemonSet has one key per node. Scrape the nodes' addresses, or use kind=Deployment" }}
{{- end }}
{{- if .Values.identity.existingSecret }}
{{- fail "identity.* does not apply to a DaemonSet: each node keeps its own identity in its state directory" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Address the HTTP output listens on. Empty `http.bind` is the loopback of
the node for a DaemonSet (host network, no TLS by default: the node's
addresses would carry the agent API in clear) and every address for a
Deployment, whose pod network is the isolation.
*/}}
{{- define "senhub-agent.httpBind" -}}
{{- if .Values.http.bind }}{{ .Values.http.bind }}
{{- else if eq (include "senhub-agent.kind" .) "DaemonSet" }}127.0.0.1
{{- else }}0.0.0.0
{{- end }}
{{- end }}

{{/*
Whether the HTTP output has TLS on (config.strategies.http.tls.enabled).
*/}}
{{- define "senhub-agent.httpTLS" -}}
{{- $s := ((.Values.config).strategies) | default dict }}
{{- $h := (get $s "http") | default dict }}
{{- if ((get $h "tls") | default dict).enabled }}true{{- end }}
{{- end }}

{{/*
A probe as configured. With a loopback bind the kubelet must call the
loopback of the node, which only a host-network pod shares: set the host
of httpGet probes to it, and their scheme to HTTPS when the HTTP output
has TLS on.
*/}}
{{- define "senhub-agent.probe" -}}
{{- $p := deepCopy .probe }}
{{- if and (has .bind (list "127.0.0.1" "::1" "localhost")) (hasKey $p "httpGet") (not (get $p.httpGet "host")) }}
{{- $_ := set $p.httpGet "host" (ternary "::1" "127.0.0.1" (eq .bind "::1")) }}
{{- end }}
{{- /* With TLS on, the listener answers plain HTTP with 400: probe it in
HTTPS (the kubelet does not verify the certificate). */}}
{{- if and .tls (hasKey $p "httpGet") (not (get $p.httpGet "scheme")) }}
{{- $_ := set $p.httpGet "scheme" "HTTPS" }}
{{- end }}
{{- toYaml $p }}
{{- end }}
