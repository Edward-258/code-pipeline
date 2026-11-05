package main

import (
	_ "embed" // go:embed []byte 形态：包名未直接引用，blank import（编译器要求）
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/api"
	"pipeline/pkg/config"
	"pipeline/pkg/engine"
	"pipeline/pkg/engine/cron"
	"pipeline/pkg/repo"
)

//go:embed deploy/web/index.html
var webIndex []byte

func main() {
	cfg := config.Load()

	if err := repo.Init(cfg.MongoURI); err != nil {
		log.Fatalf("connect mongodb failed: %v", err)
	}
	log.Printf("connected to mongodb")

	engine.Recover()            // M3.5 启动宣判：上一世遗留的 created/queued/running 档案宣判死亡（pause 不动）
	go engine.StartDispatcher() // T1.6 派发器：1s 一拍扫 queued，CAS 抢占后起 Run
	go cron.Start()             // T3.1 定时触发：对齐整分钟扫 cron 配置

	r := api.NewRouter()
	// T4.2：半吊子前端（单 HTML + fetch 轮询，不上框架），embed 进二进制随镜像走
	r.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", webIndex)
	})
	log.Printf("mini-pipeline listening on %s", cfg.HTTPAddr)
	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
