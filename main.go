package main

import (
	"log"

	"pipeline/api"
	"pipeline/config"
	"pipeline/engine"
	"pipeline/repo"
)

func main() {
	cfg := config.Load()

	if err := repo.Init(cfg.MongoURI); err != nil {
		log.Fatalf("connect mongodb failed: %v", err)
	}
	log.Printf("connected to mongodb")

	engine.Recover() // M3.5 启动宣判：上一世遗留的 created/running 档案宣判死亡（pause 不动）

	r := api.NewRouter()
	log.Printf("mini-pipeline listening on %s", cfg.HTTPAddr)
	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
