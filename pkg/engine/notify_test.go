package engine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"pipeline/pkg/models"
)

// T3.3 通知单测：httptest mock IM 机器人，验报文格式与加签算法（无真实机器人）

func TestSendNotifyFeishu(t *testing.T) {
	var gotBody map[string]any
	var gotSign string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		gotSign = r.URL.Query().Get("sign")
		fmt.Fprint(w, `{"code":0}`)
	}))
	defer srv.Close()

	cfg := &models.NotifyConfig{Name: "fs", Type: models.NotifyFeishu, WebhookURL: srv.URL, Secret: "fssh"}
	tk := &models.WorkflowTask{ID: "wf-7", WorkflowName: "wf", Status: models.StatusFailed, Error: "boom"}

	if err := sendNotify(cfg, tk); err != nil {
		t.Fatalf("sendNotify: %v", err)
	}
	content := gotBody["content"].(map[string]any)["text"].(string)
	if gotBody["msg_type"] != "text" || !strings.Contains(content, "wf-7") || !strings.Contains(content, "failed") {
		t.Errorf("feishu payload wrong: %v", gotBody)
	}
	// 加签算法：HMAC-SHA256(key="<ts>\n<secret>")，URL 参数里带 timestamp+sign
	if gotSign == "" {
		t.Fatal("feishu sign param missing")
	}
}

func TestSendNotifyDingTalk(t *testing.T) {
	var gotBody map[string]any
	var gotSign, gotTS string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		gotSign = r.URL.Query().Get("sign")
		gotTS = r.URL.Query().Get("timestamp")
		fmt.Fprint(w, `{"errcode":0}`)
	}))
	defer srv.Close()

	cfg := &models.NotifyConfig{Name: "dt", Type: models.NotifyDingTalk, WebhookURL: srv.URL, Secret: "dtsec"}
	tk := &models.WorkflowTask{ID: "wf-8", WorkflowName: "wf", Status: models.StatusPassed}

	if err := sendNotify(cfg, tk); err != nil {
		t.Fatalf("sendNotify: %v", err)
	}
	markdown := gotBody["markdown"].(map[string]any)
	if gotBody["msgtype"] != "markdown" || !strings.Contains(markdown["text"].(string), "wf-8") {
		t.Errorf("dingtalk payload wrong: %v", gotBody)
	}
	// 独立复算钉钉签名（key=secret, msg="<ts>\n<secret>"）与请求参数比对
	ts, _ := strconv.ParseInt(gotTS, 10, 64)
	mac := hmac.New(sha256.New, []byte("dtsec"))
	mac.Write([]byte(fmt.Sprintf("%d\n%s", ts, "dtsec")))
	if want := base64.StdEncoding.EncodeToString(mac.Sum(nil)); want != gotSign { // 服务端 Query().Get 已解码，比对裸 base64
		t.Errorf("dingtalk sign mismatch: got %q want %q", gotSign, want)
	}
}

// 重试：前 404 后 200（首发 + 重试 1 次的兜底路径）
func TestSendNotifyRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{"code":0}`)
	}))
	defer srv.Close()

	cfg := &models.NotifyConfig{Name: "rt", Type: models.NotifyFeishu, WebhookURL: srv.URL}
	if err := sendNotify(cfg, &models.WorkflowTask{WorkflowName: "wf"}); err != nil {
		t.Fatalf("retry should recover: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

