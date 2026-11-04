package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/config"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// T3.2 webhook 单测：签名对/错、分支不匹配、非 push 事件、无配置 404、裸奔模式

func webhookFixture(t *testing.T, hooks []*models.WebhookConfig) (*gin.Engine, string) {
	t.Helper()
	if err := repo.Init(config.Load().MongoURI); err != nil {
		t.Skipf("mongo unavailable, skip: %v", err)
	}
	gin.SetMode(gin.TestMode)
	wf := &models.Workflow{
		Name: "t32-hook",
		Stages: []*models.Stage{{
			Name: "s",
			Jobs: []*models.Job{{Name: "j", JobType: "freestyle",
				Spec: &models.FreestyleSpec{Scripts: []string{"echo hooked"}}}},
		}},
		Webhooks: hooks,
	}
	if err := repo.CreateWorkflow(wf); err != nil {
		t.Skipf("mongo unavailable, skip: %v", err)
	}
	return NewRouter(), wf.Name
}

func hookRequest(r *gin.Engine, wf, body, sig, event string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/webhook/"+wf, bytes.NewReader([]byte(body)))
	if sig != "" {
		req.Header.Set("X-Hub-Signature-256", sig)
	}
	if event != "" {
		req.Header.Set("X-GitHub-Event", event)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookSignatureFlow(t *testing.T) {
	r, wf := webhookFixture(t, []*models.WebhookConfig{
		{Repo: "Edward-258/code-pipeline", Branch: "master", Secret: "hushhush"},
	})
	payload, _ := json.Marshal(map[string]any{
		"ref": "refs/heads/master",
		"repository": map[string]any{"full_name": "Edward-258/code-pipeline"},
	})

	// 签名正确 → 触发
	w := hookRequest(r, wf, string(payload), sign(payload, "hushhush"), "push")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"triggered":true`)) {
		t.Fatalf("good signature: code=%d body=%s", w.Code, w.Body.String())
	}

	// 签名错误 → 401
	w = hookRequest(r, wf, string(payload), sign(payload, "wrong-secret"), "push")
	if w.Code != 401 {
		t.Fatalf("bad signature: code=%d body=%s", w.Code, w.Body.String())
	}

	// 篡改 body（签名对原 body 有效、对新 body 无效）→ 401
	tampered := append([]byte(nil), payload...)
	tampered = append(tampered, ' ')
	w = hookRequest(r, wf, string(tampered), sign(payload, "hushhush"), "push")
	if w.Code != 401 {
		t.Fatalf("tampered body: code=%d", w.Code)
	}

	// 非 push 事件（签名对）→ 200 不触发
	w = hookRequest(r, wf, string(payload), sign(payload, "hushhush"), "ping")
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(`"triggered":true`)) {
		t.Fatalf("ping event: code=%d body=%s", w.Code, w.Body.String())
	}

	// 分支不匹配（签名对）→ 200 不触发
	other, _ := json.Marshal(map[string]any{
		"ref":        "refs/heads/dev",
		"repository": map[string]any{"full_name": "Edward-258/code-pipeline"},
	})
	w = hookRequest(r, wf, string(other), sign(other, "hushhush"), "push")
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(`"triggered":true`)) {
		t.Fatalf("branch mismatch: code=%d body=%s", w.Code, w.Body.String())
	}

	// repo URL 形态（.git 后缀/完整 URL）与 full_name 等价匹配
	urlPayload, _ := json.Marshal(map[string]any{
		"ref":        "refs/heads/master",
		"repository": map[string]any{"full_name": "https://github.com/Edward-258/code-pipeline.git"},
	})
	w = hookRequest(r, wf, string(urlPayload), sign(urlPayload, "hushhush"), "push")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"triggered":true`)) {
		t.Fatalf("url-form repo: code=%d body=%s", w.Code, w.Body.String())
	}
}

// 裸奔模式：配置无 secret 时不验签放行；有 secret 而请求没签名 → 401
func TestWebhookNoSecretModes(t *testing.T) {
	rOpen, wfOpen := webhookFixture(t, []*models.WebhookConfig{{Repo: "o/r", Branch: "main"}})
	payload, _ := json.Marshal(map[string]any{
		"ref":        "refs/heads/main",
		"repository": map[string]any{"full_name": "o/r"},
	})
	w := hookRequest(rOpen, wfOpen, string(payload), "", "push")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"triggered":true`)) {
		t.Fatalf("open mode should accept unsigned push: code=%d body=%s", w.Code, w.Body.String())
	}

	rSealed, wfSealed := webhookFixture(t, []*models.WebhookConfig{{Repo: "o/r", Branch: "main", Secret: "s"}})
	w = hookRequest(rSealed, wfSealed, string(payload), "", "push")
	if w.Code != 401 {
		t.Fatalf("sealed mode must reject unsigned: code=%d", w.Code)
	}

	// 未配置 webhook 的图纸 → 404
	rNone, wfNone := webhookFixture(t, nil)
	w = hookRequest(rNone, wfNone, string(payload), "", "push")
	if w.Code != 404 {
		t.Fatalf("no webhook config: code=%d", w.Code)
	}
}
