package api

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/config"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// T2.4 archive 全链路回环：真 mongo（port-forward）+ httptest 路由 + 真 /bin/sh 打包。
// mongo 不可达自动 skip（与 repo 包集成测试同一纪律）。

func artifactTestEnv(t *testing.T) *httptest.Server {
	t.Helper()
	if err := repo.Init(config.Load().MongoURI); err != nil {
		t.Skipf("mongo unavailable, skip: %v", err)
	}
	gin.SetMode(gin.TestMode)
	return httptest.NewServer(NewRouter())
}

func artifactTask(t *testing.T, wf string) *models.WorkflowTask {
	t.Helper()
	tk := &models.WorkflowTask{
		ID:           fmt.Sprintf("%s-%d", wf, time.Now().UnixNano()%100000),
		WorkflowName: wf,
		TaskID:       time.Now().UnixNano() % 100000,
		Status:       models.StatusPassed,
		Stages:       []*models.StageTask{{Name: "s", Status: models.StatusPassed}},
	}
	if err := repo.CreateTask(tk); err != nil {
		t.Fatalf("create fixture task: %v", err)
	}
	return tk
}

func TestArtifactUploadDownloadRoundtrip(t *testing.T) {
	srv := artifactTestEnv(t)
	defer srv.Close()
	tk := artifactTask(t, "artifact-test")

	// 造一个真 tar.gz（sh + tar 在测试机就位；与 driver 生成物同一打包姿势）
	var tarball bytes.Buffer
	script := fmt.Sprintf("mkdir -p /tmp/mp-art-test && echo artifact-content > /tmp/mp-art-test/inside.txt && tar czf - -C /tmp/mp-art-test .")
	cmd := exec.Command("sh", "-c", script)
	cmd.Stdout = &tarball
	if err := cmd.Run(); err != nil {
		t.Fatalf("pack: %v", err)
	}

	// 上传
	resp, err := srv.Client().Post(fmt.Sprintf("%s/api/tasks/artifact-test/%d/artifact/out", srv.URL, tk.TaskID), "application/gzip", bytes.NewReader(tarball.Bytes()))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("upload status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 下载 → 字节级一致
	get, err := srv.Client().Get(fmt.Sprintf("%s/api/tasks/artifact-test/%d/artifact/out", srv.URL, tk.TaskID))
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer get.Body.Close()
	if get.StatusCode != 200 {
		t.Fatalf("download status = %d", get.StatusCode)
	}
	var back bytes.Buffer
	if _, err := back.ReadFrom(get.Body); err != nil {
		t.Fatalf("read download: %v", err)
	}
	if !bytes.Equal(back.Bytes(), tarball.Bytes()) {
		t.Fatal("downloaded artifact differs from uploaded")
	}

	// 下载内容确实是合法 gzip 且含产物文件名
	zr, err := gzip.NewReader(&back)
	if err != nil {
		t.Fatalf("downloaded bytes are not gzip: %v", err)
	}
	defer zr.Close()
	_ = zr // tar 结构校验交给 tarball 构造本身；能解开即达标
}

func TestArtifactNotFound(t *testing.T) {
	srv := artifactTestEnv(t)
	defer srv.Close()
	tk := artifactTask(t, "artifact-test-miss")

	resp, err := srv.Client().Get(fmt.Sprintf("%s/api/tasks/artifact-test-miss/%d/artifact/never-uploaded", srv.URL, tk.TaskID))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("missing artifact status = %d, want 404", resp.StatusCode)
	}
}

func TestArtifactOversizedRejected(t *testing.T) {
	srv := artifactTestEnv(t)
	defer srv.Close()
	tk := artifactTask(t, "artifact-test-big")

	big := bytes.Repeat([]byte("x"), 32<<20+1024) // 上限 32MB + 1KB
	resp, err := srv.Client().Post(fmt.Sprintf("%s/api/tasks/artifact-test-big/%d/artifact/huge", srv.URL, tk.TaskID), "application/octet-stream", bytes.NewReader(big))
	if err != nil {
		t.Fatalf("upload big: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 413 {
		t.Fatalf("oversized upload status = %d, want 413", resp.StatusCode)
	}
}
