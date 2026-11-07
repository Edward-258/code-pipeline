package repo

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"pipeline/pkg/models"
)

// 远程主机集合（2026-11-08，照 registry 的聚合根 CRUD 模式；无 is_default 那段领域
// 语义——主机没有"全局恰好 0 或 1 个"的互斥概念，纯名字簿）。
// 图纸 spec.hosts 只存名字引用，私钥每 attempt 现解析（exec.resolveHosts），
// 任务档案永远不含凭证。

// CreateHost —— 新建；重名冲突交由调用方翻译成 409
func CreateHost(h *models.Host) error {
	if h.CreatedAt.IsZero() {
		h.CreatedAt = time.Now()
	}
	_, err := hostColl.InsertOne(context.TODO(), h)
	if mongo.IsDuplicateKeyError(err) {
		return ErrExists
	}
	return err
}

// FindHost / ListHosts —— 与 env/registry 集合同构（聚合根访问模式：整条读，从不 JOIN）
func FindHost(name string) (*models.Host, error) {
	var h models.Host
	err := hostColl.FindOne(context.TODO(), bson.M{"_id": name}).Decode(&h)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func ListHosts() ([]*models.Host, error) {
	return listAll[models.Host](hostColl, bson.M{}, bson.D{{Key: "created_at", Value: -1}}) // 最新在上
}

// UpdateHost —— 全量替换（name 主键不可改）
func UpdateHost(h *models.Host) error {
	res, err := hostColl.ReplaceOne(context.TODO(), bson.M{"_id": h.Name}, h)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteHost —— 删除即终了：引用它的图纸在下次执行时报"resolve host 失败"点名
// （同 registry 引用删除的语义，不做引用计数）
func DeleteHost(name string) error {
	res, err := hostColl.DeleteOne(context.TODO(), bson.M{"_id": name})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}
