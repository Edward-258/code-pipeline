package repo

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"pipeline/models"
)

func CreateTask(t *models.WorkflowTask) error {
	_, err := TaskColl.InsertOne(context.TODO(), t)
	return err
}

// FindTask —— 按 (workflow_name, task_id) 取整棵任务树
// "整棵读"是这套数据的唯一读法（聚合根访问模式），从不跨实体 JOIN
func FindTask(workflowName string, taskID int64) (*models.WorkflowTask, error) {
	var t models.WorkflowTask
	err := TaskColl.FindOne(context.TODO(), bson.M{
		"workflow_name": workflowName,
		"task_id":       taskID,
	}).Decode(&t)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateTask —— ⭐ ack() 的落点（对照 Zadig mongodb/workflow_task_v4.go:318）
// 任务每前进一小步，就把【整棵树】一次性写回：ReplaceOne 单文档原子替换。
// 这是选 MongoDB 的核心理由之一：MySQL 下等价操作 = 一个事务 UPDATE 7-8 张表。
func UpdateTask(t *models.WorkflowTask) error {
	if t.ID == "" {
		return errors.New("update task: empty _id")
	}
	_, err := TaskColl.ReplaceOne(context.TODO(), bson.M{"_id": t.ID}, t)
	return err
}

// InCompletedTasks —— M3.5 启动宣判的扫描源（对照 Zadig workflow_task_v4.go:259 同名函数）。
// 只捞 created/running：这两种状态的看护人 goroutine 已随上一世进程死掉。
// ⚠️ 故意不含 pause——暂停是无需看护人的稳态（M3 灵魂），跨重启存活；
// Zadig 原版连 waiting-approve 一起取消，这里是有意的分叉点。
func InCompletedTasks() ([]*models.WorkflowTask, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})
	cur, err := TaskColl.Find(context.TODO(), bson.M{
		"status": bson.M{"$in": []models.Status{models.StatusCreated, models.StatusRunning}},
	}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.TODO())

	var out []*models.WorkflowTask
	for cur.Next(context.TODO()) {
		var t models.WorkflowTask
		if err := cur.Decode(&t); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, cur.Err()
}

func ListTasks(workflowName string, limit int64) ([]*models.WorkflowTask, error) {
	if limit <= 0 {
		limit = 20
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "task_id", Value: -1}}).
		SetLimit(limit).
		SetProjection(bson.M{"stages": 0, "workflow_args": 0}) // 列表页不拉整棵树
	cur, err := TaskColl.Find(context.TODO(), bson.M{"workflow_name": workflowName}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.TODO())

	var out []*models.WorkflowTask
	for cur.Next(context.TODO()) {
		var t models.WorkflowTask
		if err := cur.Decode(&t); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, cur.Err()
}
