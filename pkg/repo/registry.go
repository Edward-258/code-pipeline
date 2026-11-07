package repo

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"pipeline/pkg/models"
)

// 镜像仓库集合（2026-11-07，照 env 的聚合根 CRUD 模式）。多出的一段领域语义是
// IsDefault 全局唯一默认（Zadig registry_namespace 同款，用户拍板的"Zadig 全量制"）：
// 建立/更新时互斥翻转旧默认，删除默认时选举首个剩余补位——默认仓库决定所有裸名
// docker_build 的推送目标，"全局恰好 0 或 1 个"是这个语义能成立的前提。

// CreateRegistry —— 新建仓库；is_default 则翻转旧默认；重名冲突交由调用方翻译成 409
func CreateRegistry(r *models.Registry) error {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	if r.IsDefault {
		if err := clearDefaultRegistry(""); err != nil {
			return err
		}
	}
	_, err := registryColl.InsertOne(context.TODO(), r)
	if mongo.IsDuplicateKeyError(err) {
		return ErrExists
	}
	return err
}

// FindRegistry / ListRegistries —— 与 env 集合同构（聚合根访问模式：整条读，从不 JOIN）
func FindRegistry(name string) (*models.Registry, error) {
	var r models.Registry
	err := registryColl.FindOne(context.TODO(), bson.M{"_id": name}).Decode(&r)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func ListRegistries() ([]*models.Registry, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}) // 最新在上
	cur, err := registryColl.Find(context.TODO(), bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.TODO())

	var out []*models.Registry
	for cur.Next(context.TODO()) {
		var r models.Registry
		if err := cur.Decode(&r); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, cur.Err()
}

// FindDefaultRegistry —— 全局唯一默认仓库（没有则为 nil，裸名 docker_build 回落内置本地）
func FindDefaultRegistry() (*models.Registry, error) {
	var r models.Registry
	err := registryColl.FindOne(context.TODO(), bson.M{"is_default": true}).Decode(&r)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// UpdateRegistry —— 全量替换（name 主键不可改）；is_default 翻转语义同 Create
func UpdateRegistry(r *models.Registry) error {
	if r.IsDefault {
		if err := clearDefaultRegistry(r.Name); err != nil {
			return err
		}
	}
	res, err := registryColl.ReplaceOne(context.TODO(), bson.M{"_id": r.Name}, r)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteRegistry —— 删除即终了：删到默认仓库就回落"无默认"稳态（裸名 docker_build
// 回落内置本地）。有意分叉自 Zadig（它删默认会选举首个剩余补位）——它的语义里"没有
// 默认"会导致部署/拉取失败所以必须有人站；我们的三档推送里"无默认 = 回落内置"是
// 设计好的一等状态，强行选举反而制造"删一个仓库 → 所有裸名图纸目标静默漂到任意
// 历史记录"的意外（bootstrap-5 gate 活体抓的：共享库里选举选中了 _id 最前的生产记录）
func DeleteRegistry(name string) error {
	res, err := registryColl.DeleteOne(context.TODO(), bson.M{"_id": name})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// clearDefaultRegistry —— 清掉当前默认（exclude 非空时跳过它：Update 场景自举不误伤）
func clearDefaultRegistry(exclude string) error {
	q := bson.M{"is_default": true}
	if exclude != "" {
		q["_id"] = bson.M{"$ne": exclude}
	}
	_, err := registryColl.UpdateMany(context.TODO(), q, bson.M{"$unset": bson.M{"is_default": ""}})
	return err
}
