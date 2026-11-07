package api

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"pipeline/pkg/models"
)

// TestHostsCRUD —— 主机管理全链路回环（真 mongo + httptest，putTestServer 同纪律）：
// CRUD 全路径 + 校验 400 族。无 registry 那段 is_default 领域语义，纯名字簿，
// 不需要共享库快照恢复（只动自己名字空间内的记录）。
func TestHostsCRUD(t *testing.T) {
	base := putTestServer(t)
	nameA := fmt.Sprintf("host-%d-a", time.Now().UnixNano()%100000)
	defer httpJSON(t, "DELETE", base+"/api/hosts/"+nameA, nil)

	// 400：私钥必填（没有凭证的"主机记录"什么都连不上，不如建不出来）
	if code, _ := httpJSON(t, "POST", base+"/api/hosts", &models.Host{Name: nameA, UserName: "root", IP: "10.0.0.1"}); code != 400 {
		t.Fatalf("empty private_key: want 400, got %d", code)
	}
	// 400：ip 注入面（heredoc 展开字符白名单外的 $）
	if code, _ := httpJSON(t, "POST", base+"/api/hosts", &models.Host{Name: nameA, UserName: "root", IP: "$(reboot)", PrivateKey: "k"}); code != 400 {
		t.Fatalf("ip with shell metachars: want 400, got %d", code)
	}
	// 400：坏名字形状
	if code, _ := httpJSON(t, "POST", base+"/api/hosts", &models.Host{Name: "Bad_Name", UserName: "root", IP: "10.0.0.1", PrivateKey: "k"}); code != 400 {
		t.Fatalf("bad name shape: want 400, got %d", code)
	}

	// 200：建（port 省略 → 归一 22）
	code, body := httpJSON(t, "POST", base+"/api/hosts", &models.Host{
		Name: nameA, UserName: "root", IP: "172.29.237.250", PrivateKey: "secret-key", Desc: "live node",
	})
	if code != 200 {
		t.Fatalf("create: want 200, got %d: %s", code, body)
	}
	var got models.Host
	json.Unmarshal(body, &got)
	if got.Port != 22 {
		t.Fatalf("port default: want 22, got %d", got.Port)
	}
	if got.PrivateKey != "secret-key" {
		t.Fatalf("private_key roundtrip broken")
	}

	// 409：重名
	if code, _ = httpJSON(t, "POST", base+"/api/hosts", &models.Host{Name: nameA, UserName: "root", IP: "x", PrivateKey: "k"}); code != 409 {
		t.Fatalf("duplicate create: want 409, got %d", code)
	}
	// 400：body/path 名字不一致
	if code, _ = httpJSON(t, "PUT", base+"/api/hosts/"+nameA, &models.Host{Name: "someone-else", UserName: "root", IP: "x", PrivateKey: "k"}); code != 400 {
		t.Fatalf("name mismatch: want 400, got %d", code)
	}
	// 404：改不存在的
	if code, _ = httpJSON(t, "PUT", base+"/api/hosts/ghost-host-xyz", &models.Host{UserName: "root", IP: "x", PrivateKey: "k"}); code != 404 {
		t.Fatalf("update missing: want 404, got %d", code)
	}

	// 更新 + 列表可见（最新在上语义由 created_at 排序承担，这里只验回环）
	if code, _ = httpJSON(t, "PUT", base+"/api/hosts/"+nameA, &models.Host{
		Name: nameA, UserName: "deploy", IP: "172.29.237.249", Port: 2222, PrivateKey: "key-v2",
	}); code != 200 {
		t.Fatalf("update: want 200")
	}
	code, body = httpJSON(t, "GET", base+"/api/hosts", nil)
	if code != 200 {
		t.Fatalf("list: want 200, got %d", code)
	}
	var list []*models.Host
	json.Unmarshal(body, &list)
	var found *models.Host
	for _, h := range list {
		if h.Name == nameA {
			found = h
		}
	}
	if found == nil || found.UserName != "deploy" || found.Port != 2222 || found.PrivateKey != "key-v2" {
		t.Fatalf("updated record not in list: %+v", found)
	}

	// 删除 + 404 收尾
	if code, _ = httpJSON(t, "DELETE", base+"/api/hosts/"+nameA, nil); code != 200 {
		t.Fatalf("delete: want 200")
	}
	if code, _ = httpJSON(t, "DELETE", base+"/api/hosts/"+nameA, nil); code != 404 {
		t.Fatalf("delete missing: want 404, got %d", code)
	}
}
