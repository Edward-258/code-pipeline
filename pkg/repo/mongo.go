package repo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 三个集合，对照 Zadig 的 147 个集合里最核心的三件：
// workflow（图纸）/ workflow_task（施工档案）/ counter（发号器）
var (
	Client       *mongo.Client
	WorkflowColl *mongo.Collection
	TaskColl     *mongo.Collection
	CounterColl  *mongo.Collection
)

func Init(uri string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("mongo ping: %w", err)
	}

	Client = client
	db := client.Database("pipeline")
	WorkflowColl = db.Collection("workflow")
	TaskColl = db.Collection("workflow_task")
	CounterColl = db.Collection("counter")
	return nil
}
