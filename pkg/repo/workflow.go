package repo

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"pipeline/pkg/models"
)

var (
	ErrNotFound = errors.New("document not found")
	ErrExists   = errors.New("document already exists") // T2.1：重名资源冲突（env 等）
)

func CreateWorkflow(w *models.Workflow) error {
	w.CreatedAt = time.Now()
	w.UpdatedAt = w.CreatedAt
	_, err := workflowColl.InsertOne(context.TODO(), w)
	return err
}

func FindWorkflow(name string) (*models.Workflow, error) {
	var w models.Workflow
	err := workflowColl.FindOne(context.TODO(), bson.M{"_id": name}).Decode(&w)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func ListWorkflows() ([]*models.Workflow, error) {
	cur, err := workflowColl.Find(context.TODO(), bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.TODO())

	var out []*models.Workflow
	for cur.Next(context.TODO()) {
		var w models.Workflow
		if err := cur.Decode(&w); err != nil {
			return nil, err
		}
		out = append(out, &w)
	}
	return out, cur.Err()
}

// UpdateWorkflow —— 全量替换（与 ack 的 ReplaceOne 同构）；不存在交由调用方翻译 404。
// CreatedAt 由调用方从旧档带过来：图纸的出身不许被更新改写。
func UpdateWorkflow(w *models.Workflow) error {
	w.UpdatedAt = time.Now()
	res, err := workflowColl.ReplaceOne(context.TODO(), bson.M{"_id": w.Name}, w)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteWorkflow —— Zadig 的做法是连带软删历史任务（is_deleted: true）
// 学习版：图纸硬删，任务档案保留（审计需求，删档只应发生在保留策略里）
func DeleteWorkflow(name string) error {
	_, err := workflowColl.DeleteOne(context.TODO(), bson.M{"_id": name})
	return err
}
