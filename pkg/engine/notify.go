package engine

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// IM 通知（T3.3，对照 Zadig imnotify_service.go 的单路极简版）：
// 任务终态（engine.finish 落档时）→ fire-and-forget goroutine POST IM 机器人，
// 重试 1 次；失败只记日志——通知不阻塞不回滚任务状态（上报是尽力而为）。

// notifyOnFinish —— finish() 的挂点：拉配置、起 goroutine，绝不拖慢终态收口。
func notifyOnFinish(t *models.WorkflowTask) {
	cfgs, err := repo.ListNotifies()
	if err != nil {
		log.Printf("notify: list configs: %v", err)
		return
	}
	if len(cfgs) == 0 {
		return
	}
	tk := *t // 拷贝状态快照（goroutine 里的 t 可能继续被改）
	go func() {
		for _, cfg := range cfgs {
			if !cfg.Enabled {
				continue
			}
			if err := sendNotify(cfg, &tk); err != nil {
				log.Printf("notify %q: %v", cfg.Name, err)
			}
		}
	}()
}

// sendNotify —— 组报文 POST（带 1 次重试；HTTP 调用可注入便于单测）
var sendNotify = func(cfg *models.NotifyConfig, t *models.WorkflowTask) error {
	title := fmt.Sprintf("mini-pipeline %s", t.Status)
	text := fmt.Sprintf("**%s** 任务 `%s` %s", t.WorkflowName, t.ID, t.Status)
	if t.Error != "" {
		text += "\n> " + t.Error
	}

	var payload []byte
	switch cfg.Type {
	case models.NotifyFeishu:
		payload, _ = json.Marshal(map[string]any{
			"msg_type": "text",
			"content":  map[string]string{"text": fmt.Sprintf("%s 任务 %s %s", t.WorkflowName, t.ID, t.Status)},
		})
	case models.NotifyDingTalk:
		payload, _ = json.Marshal(map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]string{"title": title, "text": text},
		})
	default:
		return fmt.Errorf("unknown notify type %q", cfg.Type)
	}

	target := cfg.WebhookURL
	if cfg.Secret != "" {
		ts := time.Now().Unix()
		target = appendSignature(target, cfg.Type, cfg.Secret, ts)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ { // 首发 + 重试 1 次
		resp, err := notifyHTTP.Post(target, "application/json", bytes.NewReader(payload))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("im endpoint status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(time.Second) // 重试前歇一拍
	}
	return lastErr
}

// notifyHTTP —— HTTP 客户端钩子（单测注入 httptest 服务器）
var notifyHTTP = &http.Client{Timeout: 10 * time.Second}

// appendSignature —— 双平台加签（秘钥算法两家不同，别记混）：
//   飞书：HMAC-SHA256(key = "<ts>\n<secret>", msg = "") → base64，参数 ?timestamp=&sign=
//   钉钉：HMAC-SHA256(key = secret, msg = "<ts>\n<secret>") → base64+urlencode，同款参数
func appendSignature(target, typ, secret string, ts int64) string {
	var sign string
	switch typ {
	case models.NotifyFeishu:
		mac := hmac.New(sha256.New, []byte(fmt.Sprintf("%d\n%s", ts, secret)))
		sign = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	case models.NotifyDingTalk:
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(fmt.Sprintf("%d\n%s", ts, secret)))
		sign = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	}
	sep := "?"
	if u, err := url.Parse(target); err == nil && u.RawQuery != "" {
		sep = "&"
	}
	return fmt.Sprintf("%s%stimestamp=%d&sign=%s", target, sep, ts, url.QueryEscape(sign))
}
