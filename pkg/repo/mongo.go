// Package repo —— MongoDB 持久层：五集合的 CRUD、ack 全量写、counter 发号器、
// GridFS artifacts。包外不摸集合句柄，一律走这里的函数。
package repo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
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
	registryColl *mongo.Collection
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
	registryColl = db.Collection("registry")
	cronColl = db.Collection("cron")
	notifyColl = db.Collection("notify")

	// 查询面索引（CreateIndex 幂等，重复声明无害）：任务列表（workflow_name 过滤 +
	// task_id 排序）与派发器每秒扫描（status）。_id 主键查询（FindTask）天然有索引。
	_, err = taskColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "workflow_name", Value: 1}, {Key: "task_id", Value: -1}}},
		{Keys: bson.D{{Key: "status", Value: 1}}},
	})
	if err != nil {
		return fmt.Errorf("create task indexes: %w", err)
	}
	return nil
}

// ---------- 同构 CRUD 的共享件（泛型收口：五集合 + task 的 List 是同一块骨头） ----------

// decodeAll —— 游标 → []*T。全库唯一的解码循环：聚合根访问模式的"整棵读/整列读"
// 从不 JOIN，逐条 Decode 收敛在此，调用方只剩 filter/sort 的差异。
func decodeAll[T any](cur *mongo.Cursor) ([]*T, error) {
	out := []*T{}
	for cur.Next(context.TODO()) {
		var v T
		if err := cur.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, &v)
	}
	return out, cur.Err()
}

// listAll —— filter + 可选排序的整列读（sort 空 = 自然序，但集合 List 一律显式给序）
func listAll[T any](coll *mongo.Collection, filter any, sort bson.D) ([]*T, error) {
	opts := options.Find()
	if len(sort) > 0 {
		opts = opts.SetSort(sort)
	}
	cur, err := coll.Find(context.TODO(), filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.TODO())
	return decodeAll[T](cur)
}
