{{/*
Templates that take a component use: (dict "ctx" $ "component" "<name>").
*/}}

{{- define "mnemonic.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "mnemonic.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
The labels NetworkPolicies match on. They leave out app.kubernetes.io/name on
purpose: the e2e script starts probe pods with these labels to test a
component's policy, and without the name label those pods never become
endpoints of the component's Service.
*/}}
{{- define "mnemonic.policyLabels" -}}
app.kubernetes.io/instance: {{ .ctx.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "mnemonic.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mnemonic.name" .ctx }}
{{ include "mnemonic.policyLabels" . }}
{{- end -}}

{{- define "mnemonic.labels" -}}
{{ include "mnemonic.selectorLabels" . }}
app.kubernetes.io/part-of: mnemonic
app.kubernetes.io/version: {{ .ctx.Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .ctx.Release.Service }}
helm.sh/chart: {{ include "mnemonic.chart" .ctx }}
{{- end -}}

{{- define "mnemonic.credentialsSecret" -}}
{{- printf "%s-dev-credentials" .Release.Name -}}
{{- end -}}

{{/*
Checksum of ConfigMaps created outside the chart (make k8s-config), so a
change to a repo config file restarts the pods that mount it on the next
helm upgrade. lookup returns nothing under `helm template`, which only makes
the checksum constant there.
Usage: include "mnemonic.configChecksum" (dict "ctx" $ "names" (list "a" "b"))
*/}}
{{- define "mnemonic.configChecksum" -}}
{{- $data := list -}}
{{- range .names -}}
{{- $cm := lookup "v1" "ConfigMap" $.ctx.Release.Namespace . -}}
{{- $data = append $data ($cm.data | default dict) -}}
{{- end -}}
{{- $data | toJson | sha256sum -}}
{{- end -}}

{{/*
Pod-level hardening shared by every workload. No pod talks to the Kubernetes
API, and service links are off because Kubernetes would otherwise inject
variables such as NEO4J_PORT and MNEMONIC_MCP_PORT, which Neo4j and the
services' MNEMONIC_ config would both try to read.
*/}}
{{- define "mnemonic.podDefaults" -}}
automountServiceAccountToken: false
enableServiceLinks: false
{{- end -}}

{{/*
Restricted pod security context for a given numeric user.
Usage: include "mnemonic.podSecurityContext" 65532
*/}}
{{- define "mnemonic.podSecurityContext" -}}
runAsNonRoot: true
runAsUser: {{ . }}
runAsGroup: {{ . }}
fsGroup: {{ . }}
fsGroupChangePolicy: OnRootMismatch
seccompProfile:
  type: RuntimeDefault
{{- end -}}

{{/*
Restricted container security context. Pass false for images that must
write to their own filesystem (each such use is explained where it occurs).
Usage: include "mnemonic.containerSecurityContext" true
*/}}
{{- define "mnemonic.containerSecurityContext" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: {{ . }}
capabilities:
  drop: [ALL]
{{- end -}}

{{/*
Settings the three Go services share, as in docker-compose.yaml. Credentials
come from the dev Secret; the OpenAI key is only the placeholder, and all
OpenAI calls go to the egress proxy.
*/}}
{{- define "mnemonic.serviceEnv" -}}
{{- $secret := include "mnemonic.credentialsSecret" .ctx -}}
- name: MNEMONIC_DATABASE_POSTGRES_HOST
  value: {{ .ctx.Values.postgres.serviceName | quote }}
- name: MNEMONIC_DATABASE_POSTGRES_PASSWORD
  valueFrom:
    secretKeyRef: { name: {{ $secret }}, key: postgres-password }
- name: MNEMONIC_DATABASE_POSTGRES_SSL_MODE
  value: disable
- name: MNEMONIC_DATABASE_NEO4J_URI
  value: {{ printf "bolt://%s:%v" .ctx.Values.neo4j.serviceName .ctx.Values.neo4j.boltPort | quote }}
- name: MNEMONIC_DATABASE_NEO4J_PASSWORD
  valueFrom:
    secretKeyRef: { name: {{ $secret }}, key: neo4j-password }
- name: MNEMONIC_OPENAI_API_KEY
  value: {{ .ctx.Values.openai.placeholderKey | quote }}
- name: MNEMONIC_OPENAI_BASE_URL
  value: {{ printf "http://%s:%v/v1" .ctx.Values.egress.serviceName .ctx.Values.egress.port | quote }}
- name: MNEMONIC_LOGGING_LEVEL
  value: {{ .logLevel | quote }}
{{- end -}}

{{- define "mnemonic.rabbitmqEnv" -}}
{{- $secret := include "mnemonic.credentialsSecret" . -}}
- name: MNEMONIC_QUEUE_RABBITMQ_HOST
  value: {{ .Values.rabbitmq.serviceName | quote }}
- name: MNEMONIC_QUEUE_RABBITMQ_USER
  valueFrom:
    secretKeyRef: { name: {{ $secret }}, key: rabbitmq-user }
- name: MNEMONIC_QUEUE_RABBITMQ_PASSWORD
  valueFrom:
    secretKeyRef: { name: {{ $secret }}, key: rabbitmq-password }
{{- end -}}

{{/*
Probes for the Go services: ready when /health reports its dependencies up,
alive while the HTTP port accepts connections, so a database outage takes a
pod out of rotation without restarting it.
*/}}
{{- define "mnemonic.serviceProbes" -}}
startupProbe:
  httpGet: { path: /health, port: http }
  periodSeconds: 2
  failureThreshold: 60
readinessProbe:
  httpGet: { path: /health, port: http }
  periodSeconds: 10
  timeoutSeconds: 5
livenessProbe:
  tcpSocket: { port: http }
  periodSeconds: 20
  timeoutSeconds: 5
{{- end -}}
