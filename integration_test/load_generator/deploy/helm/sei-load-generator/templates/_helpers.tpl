{{- define "sei-load-generator.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "sei-load-generator.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "sei-load-generator.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "sei-load-generator.image" -}}
{{- printf "%s:%s" .Values.image.repository .Values.image.tag -}}
{{- end -}}

{{- define "sei-load-generator.fixtureClaimName" -}}
{{- printf "%s-fixtures-%s" (include "sei-load-generator.fullname" .root) .set | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "sei-load-generator.fixtureSetupName" -}}
{{- printf "%s-setup-%s-%v" (include "sei-load-generator.fullname" .root) .set .root.Release.Revision | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "sei-load-generator.targetEnv" -}}
- name: TARGET_NETWORK
  value: {{ .Values.target.network | quote }}
{{- if .Values.target.evmRpcUrl }}
- name: TARGET_EVM_RPC
  value: {{ .Values.target.evmRpcUrl | quote }}
{{- end }}
{{- if .Values.target.cosmosRpcUrl }}
- name: TARGET_COSMOS_RPC
  value: {{ .Values.target.cosmosRpcUrl | quote }}
{{- end }}
{{- end -}}

{{- define "sei-load-generator.containerSecurityContext" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
runAsNonRoot: true
runAsUser: 1000
capabilities:
  drop:
    - ALL
{{- end -}}
