package config

import "os"

type Config struct {
	MongoURI     string // MongoDB 连接串
	HTTPAddr     string // API 监听地址
	K8SNamespace string // K8s Job 所在 namespace（M1+）
	K8SContext   string // 仅本机调试用：kubeconfig 的 context 名；引擎在集群内跑时留空走 InClusterConfig
}

func Load() *Config {
	return &Config{
		// 默认指向集群内 svc DNS：引擎跑在 k3s 里全内网通信。
		// 本机调试姿势：先 port-forward 再用 localhost 覆盖——
		//   kubectl --context k3s-heyuan port-forward -n pipeline svc/mongo 27017:27017
		//   $env:MONGO_URI="mongodb://root:mini-pipeline-2026@localhost:27017"
		MongoURI:     getenv("MONGO_URI", "mongodb://root:mini-pipeline-2026@mongo.pipeline.svc.cluster.local:27017"),
		HTTPAddr:     getenv("HTTP_ADDR", ":8080"),
		K8SNamespace: getenv("K8S_NAMESPACE", "pipeline"),
		K8SContext:   getenv("K8S_CONTEXT", ""),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
