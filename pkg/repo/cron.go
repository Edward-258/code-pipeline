package repo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"pipeline/pkg/models"
)

func CreateCron(c *models.Cron) error {
	now := time.Now()
	c.CreatedAt, c.UpdatedAt = now, now
	_, err := CronColl.InsertOne(context.TODO(), c)
	if mongo.IsDuplicateKeyError(err) {
		return ErrExists
	}
	return err
}

func FindCron(name string) (*models.Cron, error) {
	var c models.Cron
	err := CronColl.FindOne(context.TODO(), bson.M{"_id": name}).Decode(&c)
	if err == mongo.ErrNoDocuments {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func ListCrons() ([]*models.Cron, error) {
	cur, err := CronColl.Find(context.TODO(), bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.TODO())

	var out []*models.Cron
	for cur.Next(context.TODO()) {
		var c models.Cron
		if err := cur.Decode(&c); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, cur.Err()
}

// UpdateCron —— 全量替换（enabled/crontab/workflow 都可改），updatedAt 刷新
func UpdateCron(c *models.Cron) error {
	c.UpdatedAt = time.Now()
	_, err := CronColl.ReplaceOne(context.TODO(), bson.M{"_id": c.Name}, c)
	return err
}

func DeleteCron(name string) error {
	res, err := CronColl.DeleteOne(context.TODO(), bson.M{"_id": name})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}
