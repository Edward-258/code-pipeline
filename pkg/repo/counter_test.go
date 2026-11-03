package repo

import (
	"testing"

	"pipeline/pkg/config"
)

// TestNextID —— 发号器验收：连续两次发号必须严格 +1（原子性）
// 直连集群里的 Mongo，属于集成测试。
// mongo 已收编进 helm chart 且只暴露 ClusterIP，本机跑测试需先开隧道：
//
//	kubectl --context k3s-heyuan port-forward -n pipeline svc/mongo 27017:27017
//	$env:MONGO_URI="mongodb://root:mini-pipeline-2026@localhost:27017"
func TestNextID(t *testing.T) {
	if err := Init(config.Load().MongoURI); err != nil {
		t.Skipf("mongo unavailable, skip: %v", err)
	}

	a, err := NextID("workflow_task:counter-test")
	if err != nil {
		t.Fatalf("NextID first call: %v", err)
	}
	b, err := NextID("workflow_task:counter-test")
	if err != nil {
		t.Fatalf("NextID second call: %v", err)
	}
	if b != a+1 {
		t.Fatalf("expected seq strictly +1, got %d then %d", a, b)
	}
	t.Logf("counter works: %d -> %d", a, b)
}
