package repo

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"

	"pipeline/models"
)

// NextID —— 原子发号（对照 Zadig mongodb/counter.go 的 GetNextSeq）
//
// FindOneAndUpdate + $inc 是 MongoDB 单文档原子操作：
// 并发 100 个请求同时来，seq 依然严格递增不重号——这就是没有自增主键的代价与解法。
// Upsert(true)：第一个号自动建档；ReturnDocument(After)：直接返回递增后的值。
func NextID(key string) (int64, error) {
	var c models.Counter
	err := CounterColl.FindOneAndUpdate(
		context.TODO(),
		bson.M{"_id": key},
		bson.M{"$inc": bson.M{"seq": int64(1)}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&c)
	if err != nil {
		return 0, err
	}
	return c.Seq, nil
}
