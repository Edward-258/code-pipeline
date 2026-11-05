package models

import "time"

// Env —— 部署目标环境（T2.1，对照 Zadig models/product.go 的 ProductEnv 砍到骨头）：
// 一个 env = 集群里的一个 namespace。deploy job（T2.2）拿它决定 patch 哪个 ns 的 Deployment。
// 学习版差异：不做集群维度（单集群）、不做服务清单/状态回收，就是一张"名字 → namespace"的映射表。
type Env struct {
	Name      string    `bson:"_id"        json:"name"` // env 名即主键
	Namespace string    `bson:"namespace"  json:"namespace"`
	Desc      string    `bson:"desc,omitempty" json:"desc,omitempty"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}
