// Package repo —— MongoDB 持久层：五集合的 CRUD、ack 全量写、counter 发号器、
// GridFS artifacts。包外不摸集合句柄，一律走这里的函数。
package repo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDB 连接与集合清单：对照 Zadig 的 147 个集合里最核心的几件：
// workflow（图纸）/ workflow_task（施工档案）/ counter（发号器）/ env（部署目标，T2.1）。
// 全部包内私有——包外的存取一律走下面的 CRUD 函数，不直接摸集合句柄。
var (
	client       *mongo.Client
	workflowColl *mongo.Collection
	taskColl     *mongo.Collection
	counterColl  *mongo.Collection
	envColl      *mongo.Collection
	cronColl     *mongo.Collection
	notifyColl   *mongo.Collection
)

// Init —— 连接 mongo 并装配全部集合句柄（main 启动单点调用；失败即 Fatal）
func Init(uri string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return fmt.Errorf("mongo connect: %w", err)
	}
	if err := c.Ping(ctx, nil); err != nil {
		return fmt.Errorf("mongo ping: %w", err)
	}

	client = c
	db := c.Database("pipeline")
	workflowColl = db.Collection("workflow")
	taskColl = db.Collection("workflow_task")
	counterColl = db.Collection("counter")
	envColl = db.Collection("env")
	cronColl = db.Collection("cron")
	notifyColl = db.Collection("notify")
	return nil
}
