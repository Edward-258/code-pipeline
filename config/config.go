package config

import "os"

type Config struct {
	MongoURI  string // MongoDB 连接串
	HTTPAddr  string // API 监听地址
	K8SNamespace string // M1 起 K8s Job 落在哪个 namespace
}

func Load() *Config {
	return &Config{
		MongoURI:     getenv("MONGO_URI", "mongodb://root:mini-pipeline-2026@8.155.137.224:30017"),
		HTTPAddr:     getenv("HTTP_ADDR", ":8080"),
		K8SNamespace: getenv("K8S_NAMESPACE", "pipeline"),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
