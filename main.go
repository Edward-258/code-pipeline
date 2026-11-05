package main

import (
	"log"

	"pipeline/pkg/api"
	"pipeline/pkg/config"
	"pipeline/pkg/engine"
	"pipeline/pkg/engine/cron"
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
	r := api.NewRouter()
	log.Printf("mini-pipeline listening on %s", cfg.HTTPAddr)
	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
