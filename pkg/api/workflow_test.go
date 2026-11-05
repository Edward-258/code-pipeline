package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/config"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// PUT workflow/env 全链路回环：真 mongo（port-forward）+ httptest 路由，
// mongo 不可达自动 skip（与 artifact_test 同一纪律）。

func putTestServer(t *testing.T) string {
	t.Helper()
	if err := repo.Init(config.Load().MongoURI); err != nil {
		t.Skipf("mongo unavailable, skip: %v", err)
	}
	gin.SetMode(gin.TestMode)
	return httptest.NewServer(NewRouter()).URL
}

func httpJSON(t *testing.T, method, url string, body any) (int, []byte) {
	t.Helper()
	var rd *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rd = bytes.NewBuffer(b)
	} else {
		rd = bytes.NewBuffer(nil)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()
	var out bytes.Buffer
	out.ReadFrom(res.Body)
	return res.StatusCode, out.Bytes()
}

func putFixtureWorkflow(name string) *models.Workflow {
	return &models.Workflow{
		Name: name,
		Stages: []*models.Stage{{
			Name: "s1",
			Jobs: []*models.Job{{
				Name: "j1", JobType: models.JobTypeFreestyle,
				Spec: &models.FreestyleSpec{Steps: []*models.StepSpec{
					{Name: "hi", Type: models.StepShell, Script: "echo hi"},
				}},
			}},
		}},
	}
}

// TestUpdateWorkflow —— 建档→PUT 改图纸→GET 验证（内容更新 + created_at 出身保留）+ 404/400 全路径
func TestUpdateWorkflow(t *testing.T) {
	base := putTestServer(t)
	name := fmt.Sprintf("putwf-%d", time.Now().UnixNano()%100000)
	defer httpJSON(t, "DELETE", base+"/api/workflows/"+name, nil)

	if code, _ := httpJSON(t, "POST", base+"/api/workflows", putFixtureWorkflow(name)); code != 200 {
		t.Fatalf("create workflow: want 200, got %d", code)
	}

	// PUT：从 1 stage 扩成 2 stage（body.name 省略，路径名为准）
	upd := putFixtureWorkflow("")
	upd.Stages = append(upd.Stages, &models.Stage{
		Name: "s2",
		Jobs: []*models.Job{{
			Name: "deploy", JobType: models.JobTypeDeploy,
			DeploySpec: &models.DeploySpec{Env: "dev", DeploymentName: "d", ContainerName: "c", Image: "img:1"},
		}},
	})
	code, body := httpJSON(t, "PUT", base+"/api/workflows/"+name, upd)
	if code != 200 {
		t.Fatalf("update workflow: want 200, got %d: %s", code, body)
	}

	code, body = httpJSON(t, "GET", base+"/api/workflows/"+name, nil)
	if code != 200 {
		t.Fatalf("get workflow: want 200, got %d", code)
	}
	var got models.Workflow
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != name || len(got.Stages) != 2 {
		t.Fatalf("update did not stick: name=%s stages=%d", got.Name, len(got.Stages))
	}
	if got.CreatedAt.IsZero() {
		t.Fatalf("created_at lost after PUT (出身不许被改写)")
	}
	if !got.UpdatedAt.After(got.CreatedAt) && !got.UpdatedAt.Equal(got.CreatedAt) {
		t.Fatalf("updated_at not refreshed: created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
	}

	// 404：改不存在的
	if code, _ := httpJSON(t, "PUT", base+"/api/workflows/putwf-nobody-home", upd); code != 404 {
		t.Fatalf("update missing workflow: want 404, got %d", code)
	}
	// 400：body.name 与路径不一致（编辑器拿错对象防呆）
	if code, _ := httpJSON(t, "PUT", base+"/api/workflows/"+name, putFixtureWorkflow("someone-else")); code != 400 {
		t.Fatalf("name mismatch: want 400, got %d", code)
	}
	// 400：坏图纸（stage 清空）
	bad := putFixtureWorkflow("")
	bad.Stages = nil
	if code, _ := httpJSON(t, "PUT", base+"/api/workflows/"+name, bad); code != 400 {
		t.Fatalf("invalid blueprint: want 400, got %d", code)
	}
}

// TestUpdateEnv —— 建档→PUT 改 namespace/desc→GET 验证（created_at 保留）+ 404/400
func TestUpdateEnv(t *testing.T) {
	base := putTestServer(t)
	name := fmt.Sprintf("putenv-%d", time.Now().UnixNano()%100000)
	defer httpJSON(t, "DELETE", base+"/api/envs/"+name, nil)

	if code, _ := httpJSON(t, "POST", base+"/api/envs", &models.Env{Name: name, Namespace: "ns-old", Desc: "before"}); code != 200 {
		t.Fatalf("create env: want 200, got %d", code)
	}

	code, body := httpJSON(t, "PUT", base+"/api/envs/"+name, &models.Env{Namespace: "ns-new", Desc: "after"})
	if code != 200 {
		t.Fatalf("update env: want 200, got %d: %s", code, body)
	}

	code, body = httpJSON(t, "GET", base+"/api/envs", nil)
	if code != 200 {
		t.Fatalf("list envs: want 200, got %d", code)
	}
	var envs []*models.Env
	if err := json.Unmarshal(body, &envs); err != nil {
		t.Fatalf("unmarshal envs: %v", err)
	}
	var got *models.Env
	for _, e := range envs {
		if e.Name == name {
			got = e
		}
	}
	if got == nil {
		t.Fatalf("env %s missing after update", name)
	}
	if got.Namespace != "ns-new" || got.Desc != "after" {
		t.Fatalf("env update did not stick: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Fatalf("env created_at lost after PUT")
	}

	// 404 / 400
	if code, _ := httpJSON(t, "PUT", base+"/api/envs/putenv-nobody-home", &models.Env{Namespace: "x"}); code != 404 {
		t.Fatalf("update missing env: want 404, got %d", code)
	}
	if code, _ := httpJSON(t, "PUT", base+"/api/envs/"+name, &models.Env{Namespace: ""}); code != 400 {
		t.Fatalf("empty namespace: want 400, got %d", code)
	}
	if code, _ := httpJSON(t, "PUT", base+"/api/envs/"+name, &models.Env{Namespace: "x", Name: "someone-else"}); code != 400 {
		t.Fatalf("env name mismatch: want 400, got %d", code)
	}
}
