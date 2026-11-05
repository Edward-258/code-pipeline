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

// CreateEnv —— 新建 env；重名冲突交由调用方翻译成 409
func CreateEnv(e *models.Env) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	_, err := envColl.InsertOne(context.TODO(), e)
	if mongo.IsDuplicateKeyError(err) {
		return ErrExists
	}
	return err
}

// FindEnv / ListEnvs / DeleteEnv —— 与 workflow 集合同构的 CRUD（聚合根访问模式：整条读，从不 JOIN）
func FindEnv(name string) (*models.Env, error) {
	var e models.Env
	err := envColl.FindOne(context.TODO(), bson.M{"_id": name}).Decode(&e)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func ListEnvs() ([]*models.Env, error) {
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: 1}})
	cur, err := envColl.Find(context.TODO(), bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.TODO())

	var out []*models.Env
	for cur.Next(context.TODO()) {
		var e models.Env
		if err := cur.Decode(&e); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, cur.Err()
}

// UpdateEnv —— 全量替换（name 是主键不可改，调用方已按路径名归一）；CreatedAt 同 workflow 纪律由旧档带过来
func UpdateEnv(e *models.Env) error {
	res, err := envColl.ReplaceOne(context.TODO(), bson.M{"_id": e.Name}, e)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func DeleteEnv(name string) error {
	res, err := envColl.DeleteOne(context.TODO(), bson.M{"_id": name})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}
