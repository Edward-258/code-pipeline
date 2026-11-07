package main

import (
	"log"

	"pipeline/pkg/api"
	"pipeline/pkg/config"
	"pipeline/pkg/engine"
	"pipeline/pkg/engine/cron"
	"pipeline/pkg/k8s"
	"pipeline/pkg/repo"
)

func main() {
	cfg := config.Load()

	if err := repo.Init(cfg.MongoURI); err != nil {
		log.Fatalf("connect mongodb failed: %v", err)
	}
	log.Printf("connected to mongodb")

	engine.Recover()            // M3.5 启动宣判：上一世遗留的 created/queued/running 档案宣判死亡（pause 不动）
	go engine.StartDispatcher() // T1.6 派发器：1s 一拍扫 queued，CAS 抢占后起 Run
	go cron.Start(engine.TriggerWorkflow)             // T3.1 定时触发：对齐整分钟扫 cron 配置

	// 前端已独立成 nginx 镜像 Pod（frontend/ + chart tags.web），经 ingress 统一入口
	// 反代 /api 到本引擎——引擎只服务 API（go:embed 时代 2026-11-06 收档）

	// env 随建 namespace（Zadig 同款语义，§4 决断 #13）：api 层钩子在此装配真身
	// （api 不 import K8s，router.go 教义不破）。每次现建 client = TailJobLog 同款
	// 自给风格；env 创建是低频操作，可接受。
	api.EnsureNamespaceHook = func(ns string) error {
		cli, err := k8s.NewClient(cfg.K8SContext)
		if err != nil {
			return err
		}
		return k8s.EnsureNamespace(cli, ns)
	}

	r := api.NewRouter()
	log.Printf("mini-pipeline listening on %s", cfg.HTTPAddr)
	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
