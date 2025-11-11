{{- define "k8s-copycat.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "k8s-copycat.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "k8s-copycat.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "k8s-copycat.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version -}}
{{- end -}}

{{- define "k8s-copycat.labels" -}}
helm.sh/chart: {{ include "k8s-copycat.chart" . }}
{{ include "k8s-copycat.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- end -}}

{{- define "k8s-copycat.selectorLabels" -}}
app.kubernetes.io/name: {{ include "k8s-copycat.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "k8s-copycat.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "k8s-copycat.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "k8s-copycat.configMapName" -}}
{{- if .Values.configMap.existingConfigMap -}}
{{- .Values.configMap.existingConfigMap -}}
{{- else -}}
{{- include "k8s-copycat.fullname" . -}}
{{- end -}}
{{- end -}}

{{- define "k8s-copycat.renderConfig" -}}
{{- if .Values.config -}}
{{- toYaml .Values.config -}}
{{- else -}}
{}
{{- end -}}
{{- end -}}
