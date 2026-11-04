package config

import (
	"os"
	"strconv"
)

type Config struct {
	MongoURI     string // MongoDB 连接串
	HTTPAddr     string // API 监听地址
	K8SNamespace string // K8s Job 所在 namespace（M1+）
	K8SContext   string // 仅本机调试用：kubeconfig 的 context 名；引擎在集群内跑时留空走 InClusterConfig
	RunnerImage  string // 步骤执行器宿体镜像（git/docker_build 步骤的默认容器；chart values runner.image 注入）

	MaxRunningTasks int // T1.6 全局并发上限（0 = 停止派发只收尾，维护用）
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
		// pipeline-runner（docker/runner.Dockerfile）2026-11-04 起走 ACR，与引擎镜像同实例同 namespace
		// （codepipeline-learning）。经 env 注入的原因：runner 升 tag 只改 chart values，不用重编引擎。
		// 本机调试时可直接 RUNNER_IMAGE=busybox 之类覆盖。
		RunnerImage: getenv("RUNNER_IMAGE", "crpi-28fqgwft325ylccc-vpc.cn-heyuan.personal.cr.aliyuncs.com/codepipeline-learning/pipeline-runner:0.1.0"),

		MaxRunningTasks: getenvInt("MAX_RUNNING_TASKS", 3),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return def
}
