{{/* mongoURI —— 内置 mongo 的连接串；外部 URI 优先（对齐 Zadig connections.mongodb.connectionString） */}}
{{- define "mini-pipeline.mongoURI" -}}
{{- if .Values.connections.mongo.uri -}}
{{- .Values.connections.mongo.uri -}}
{{- else -}}
mongodb://root:{{ .Values.mongo.rootPassword }}@mongo.{{ .Release.Namespace }}.svc.cluster.local:27017
{{- end -}}
{{- end -}}
