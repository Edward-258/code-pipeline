package models

// Counter —— 发号器（1:1 复刻 Zadig models/counter.go，22 行的咽喉）
// MongoDB 没有自增主键，一切业务 ID（taskID 等）都从这一个集合发号：
//
//	{ _id: "workflow_task:demo", seq: 42 }
type Counter struct {
	ID  string `bson:"_id"`
	Seq int64  `bson:"seq"`
}
