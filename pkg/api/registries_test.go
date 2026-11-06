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
	// 共享库隔离：库里有生产记录（如 acr/local-ingress），测试建默认记录会翻转它们的
	// 默认位——先快照，defer 按快照恢复（bootstrap-5 gate 活体教训：不能假设库里只有自己的数据）
	snapshot := map[string]bool{}
	code0, body0 := httpJSON(t, "GET", base+"/api/registries", nil)
	if code0 == 200 {
		var pre []*models.Registry
		json.Unmarshal(body0, &pre)
		for _, r := range pre {
			snapshot[r.Name] = r.IsDefault
		}
	}
	defer func() {
		for name, wasDefault := range snapshot {
			if name == nameA || name == nameB {
				continue
			}
			cur := listOne(t, base, name)
			if cur != nil && cur.IsDefault != wasDefault {
				cur.IsDefault = wasDefault
				httpJSON(t, "PUT", base+"/api/registries/"+name, cur)
			}
		}
	}()

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

	// 删除默认 B → 回落"无默认"稳态（有意分叉自 Zadig 的选举补位：我们的三档推送里
	// 无默认 = 裸名回落内置本地，是设计好的一等状态；强行选举会让裸名目标静默漂到
	// 任意历史记录——bootstrap-5 gate 在共享库里活体抓的）
	if code, _ = httpJSON(t, "DELETE", base+"/api/registries/"+nameB, nil); code != 200 {
		t.Fatalf("delete B: want 200, got %d", code)
	}
	if got := mustReg(t, base, nameA); got.IsDefault {
		t.Fatalf("no election expected after deleting default B (fall back to no-default steady state)")
	}
	// 404：删不存在的
	if code, _ = httpJSON(t, "DELETE", base+"/api/registries/"+nameB, nil); code != 404 {
		t.Fatalf("delete missing: want 404, got %d", code)
	}
}

// listOne —— 按名取单条（不存在返回 nil；隔离恢复逻辑用）
func listOne(t *testing.T, base, name string) *models.Registry {
	t.Helper()
	code, body := httpJSON(t, "GET", base+"/api/registries", nil)
	if code != 200 {
		return nil
	}
	var list []*models.Registry
	json.Unmarshal(body, &list)
	for _, r := range list {
		if r.Name == name {
			return r
		}
	}
	return nil
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
