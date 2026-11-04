package main

import (
	"log"

	"pipeline/pkg/api"
	"pipeline/pkg/config"
	"pipeline/pkg/engine"
	"pipeline/pkg/repo"
)

func main() {
	cfg := config.Load()

	if err := repo.Init(cfg.MongoURI); err != nil {
		log.Fatalf("connect mongodb failed: %v", err)
	}
	log.Printf("connected to mongodb")

	engine.Recover() // M3.5 启动宣判：上一世遗留的 created/queued/running 档案宣判死亡（pause 不动）
	go engine.StartDispatcher() // T1.6 派发器：1s 一拍扫 queued，CAS 抢占后起 Run

	r := api.NewRouter()
	log.Printf("mini-pipeline listening on %s", cfg.HTTPAddr)
	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
