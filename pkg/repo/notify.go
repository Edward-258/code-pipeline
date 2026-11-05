package repo

import (
	"errors"

	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"pipeline/pkg/models"
)

func CreateNotify(n *models.NotifyConfig) error {
	now := time.Now()
	n.CreatedAt, n.UpdatedAt = now, now
	_, err := notifyColl.InsertOne(context.TODO(), n)
	if mongo.IsDuplicateKeyError(err) {
		return ErrExists
	}
	return err
}

func FindNotify(name string) (*models.NotifyConfig, error) {
	var n models.NotifyConfig
	err := notifyColl.FindOne(context.TODO(), bson.M{"_id": name}).Decode(&n)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func ListNotifies() ([]*models.NotifyConfig, error) {
	cur, err := notifyColl.Find(context.TODO(), bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.TODO())

	var out []*models.NotifyConfig
	for cur.Next(context.TODO()) {
		var n models.NotifyConfig
		if err := cur.Decode(&n); err != nil {
			return nil, err
		}
		out = append(out, &n)
	}
	return out, cur.Err()
}

func UpdateNotify(n *models.NotifyConfig) error {
	n.UpdatedAt = time.Now()
	_, err := notifyColl.ReplaceOne(context.TODO(), bson.M{"_id": n.Name}, n)
	return err
}

func DeleteNotify(name string) error {
	res, err := notifyColl.DeleteOne(context.TODO(), bson.M{"_id": name})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}
