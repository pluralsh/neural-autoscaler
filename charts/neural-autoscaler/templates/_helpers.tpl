{{/*
Expand the name of the chart.
*/}}
{{- define "neural-autoscaler.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "neural-autoscaler.fullname" -}}
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

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "neural-autoscaler.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "neural-autoscaler.labels" -}}
helm.sh/chart: {{ include "neural-autoscaler.chart" . }}
{{ include "neural-autoscaler.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: neural-autoscaler
{{- end }}

{{/*
Selector labels
*/}}
{{- define "neural-autoscaler.selectorLabels" -}}
app.kubernetes.io/name: {{ include "neural-autoscaler.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: neural-autoscaler
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "neural-autoscaler.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (printf "%s" (include "neural-autoscaler.fullname" .)) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Default ONNX model path baked into the manager image.
*/}}
{{- define "neural-autoscaler.defaultModelPath" -}}
/models/chronos-2-onnx/model.onnx
{{- end }}
