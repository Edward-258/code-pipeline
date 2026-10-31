package repo

import (
	"testing"

	"pipeline/config"
)

// TestNextID —— 发号器验收：连续两次发号必须严格 +1（原子性）
// 直连实验室 Mongo，属于集成测试
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
