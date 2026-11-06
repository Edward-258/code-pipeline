package api

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"pipeline/pkg/models"
)

// TestRegistriesCRUD —— 仓库管理全链路回环（真 mongo + httptest，putTestServer 同纪律）：
// CRUD 全路径 + is_default 全量制语义（建立翻转旧默认 / 更新改靶 / 删除默认选举补位）。
func TestRegistriesCRUD(t *testing.T) {
	base := putTestServer(t)
	pfx := fmt.Sprintf("reg-%d", time.Now().UnixNano()%100000)
	nameA, nameB := pfx+"-a", pfx+"-b"
	defer httpJSON(t, "DELETE", base+"/api/registries/"+nameA, nil)
	defer httpJSON(t, "DELETE", base+"/api/registries/"+nameB, nil)

	// 400：address 必填
	if code, _ := httpJSON(t, "POST", base+"/api/registries", &models.Registry{Name: nameA}); code != 400 {
		t.Fatalf("empty address: want 400, got %d", code)
	}
	// 400：地址 scheme 被剥后仍合法才放行（规范化是 Validate 的一部分）
	code, body := httpJSON(t, "POST", base+"/api/registries", &models.Registry{
		Name: nameA, Address: "https://reg-a.example.com", Username: "u", Password: "p", IsDefault: true,
	})
	if code != 200 {
		t.Fatalf("create A: want 200, got %d: %s", code, body)
	}
	var gotA models.Registry
	json.Unmarshal(body, &gotA)
	if gotA.Address != "reg-a.example.com" {
		t.Fatalf("scheme not stripped: %q", gotA.Address)
	}
	if !gotA.IsDefault {
		t.Fatalf("A should be default (first registry)")
	}

	// B 标默认 → A 的默认位被翻转（全局唯一）
	if code, _ = httpJSON(t, "POST", base+"/api/registries", &models.Registry{
		Name: nameB, Address: "reg-b.example.com", IsDefault: true,
	}); code != 200 {
		t.Fatalf("create B: want 200, got %d", code)
	}
	defA, defB := mustReg(t, base, nameA), mustReg(t, base, nameB)
	if defA.IsDefault || !defB.IsDefault {
		t.Fatalf("default flip failed: A=%v B=%v", defA.IsDefault, defB.IsDefault)
	}

	// 409：重名
	if code, _ = httpJSON(t, "POST", base+"/api/registries", &models.Registry{Name: nameA, Address: "x"}); code != 409 {
		t.Fatalf("duplicate create: want 409, got %d", code)
	}
	// 400：body/path 名字不一致
	if code, _ = httpJSON(t, "PUT", base+"/api/registries/"+nameA, &models.Registry{Name: "someone-else", Address: "x"}); code != 400 {
		t.Fatalf("name mismatch: want 400, got %d", code)
	}

	// 删除默认 B → 选举 A 补位（默认位永远有人站）
	if code, _ = httpJSON(t, "DELETE", base+"/api/registries/"+nameB, nil); code != 200 {
		t.Fatalf("delete B: want 200, got %d", code)
	}
	if got := mustReg(t, base, nameA); !got.IsDefault {
		t.Fatalf("A should be elected default after deleting B")
	}
	// 404：删不存在的
	if code, _ = httpJSON(t, "DELETE", base+"/api/registries/"+nameB, nil); code != 404 {
		t.Fatalf("delete missing: want 404, got %d", code)
	}
}

func mustReg(t *testing.T, base, name string) *models.Registry {
	t.Helper()
	code, body := httpJSON(t, "GET", base+"/api/registries", nil)
	if code != 200 {
		t.Fatalf("list registries: want 200, got %d", code)
	}
	var list []*models.Registry
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, r := range list {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("registry %s not in list", name)
	return nil
}
