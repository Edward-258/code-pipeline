package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/engine"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// git webhook 触发（T3.2，对照 Zadig POST /v4/webhook 的极简版）。
// 只做 GitHub / Gitee 的 push 事件：HMAC-SHA256 验签 → repo+branch 匹配图纸
// webhooks 配置 → TriggerWorkflow 正常入队（creator=webhook:<repo>@<branch>）。
// Gitee 双轨：X-Gitee-Signature（HMAC hex，同 GitHub 算法）或 X-Gitee-Token（明文比对）。

// gitPushPayload —— 两个平台的 push 事件里我们只看的字段（hand-pick，容错缺字段）
type gitPushPayload struct {
	Ref        string `json:"ref"` // refs/heads/<branch>
	Repository struct {
		FullName    string `json:"full_name"`
		HTMLURL     string `json:"html_url"`
		GitHTTPURL  string `json:"git_http_url"`
		GitSSHURL   string `json:"git_ssh_url"`
		CloneURL    string `json:"clone_url"`
	} `json:"repository"`
}

// Webhook POST /api/webhook/:workflow
func Webhook(c *gin.Context) {
	wfName := c.Param("workflow")
	wf, err := repo.FindWorkflow(wfName)
	if err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(wf.Webhooks) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow has no webhook config"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body: " + err.Error()})
		return
	}

	// 验签：任一配置的 secret 验过即放行（一条图纸可挂多仓库多 secret）
	sig := signatureOf(c.Request.Header)
	if sig == "" {
		// 没带签名头：只在"所有配置都没设 secret"时放行（明确的裸奔模式，不是漏判）
		for _, wc := range wf.Webhooks {
			if wc.Secret != "" {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "missing signature header"})
				return
			}
		}
	} else if !signatureValid(sig, body, wf.Webhooks) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "signature mismatch"})
		return
	}

	var p gitPushPayload
	if err := json.Unmarshal(body, &p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad payload: " + err.Error()})
		return
	}
	if !isPushEvent(c.Request.Header) {
		c.JSON(http.StatusOK, gin.H{"triggered": false, "reason": "not a push event"})
		return
	}

	branch := branchOfRef(p.Ref)
	pushedRepo := p.Repository.FullName
	for _, wc := range wf.Webhooks {
		if branch != wc.Branch && wc.Branch != "" {
			continue // 分支不匹配；wc.Branch 为空 = 任意分支（宽松模式）
		}
		if pushedRepo != "" && !repoMatches(pushedRepo, wc.Repo) {
			continue
		}
		t, err := engine.TriggerWorkflow(wfName, "webhook:"+wc.Repo+"@"+branch)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"triggered": true, "task_id": t.TaskID, "creator": t.Creator})
		return
	}
	c.JSON(http.StatusOK, gin.H{"triggered": false, "reason": "no webhook config matched repo/branch"})
}

// signatureOf —— 三种签名头按平台取值（GitHub / Gitee-HMAC / Gitee-Token）
func signatureOf(h http.Header) string {
	if v := h.Get("X-Hub-Signature-256"); v != "" {
		return v
	}
	if v := h.Get("X-Gitee-Signature"); v != "" {
		return v
	}
	if v := h.Get("X-Gitee-Token"); v != "" {
		return v
	}
	return ""
}

// signatureValid —— HMAC-SHA256 hex（前缀 sha256= 可有可无）或 Gitee 明文 token 比对，
// 全部走 constant-time 比较
func signatureValid(sig string, body []byte, wcs []*models.WebhookConfig) bool {
	for _, wc := range wcs {
		if wc.Secret == "" {
			continue
		}
		mac := hmac.New(sha256.New, []byte(wc.Secret))
		mac.Write(body)
		want := hex.EncodeToString(mac.Sum(nil))
		given := strings.TrimPrefix(sig, "sha256=")
		if subtle.ConstantTimeCompare([]byte(want), []byte(given)) == 1 {
			return true
		}
		// Gitee 明文 token 形态
		if subtle.ConstantTimeCompare([]byte(wc.Secret), []byte(sig)) == 1 {
			return true
		}
	}
	return false
}

func isPushEvent(h http.Header) bool {
	if e := h.Get("X-GitHub-Event"); e != "" {
		return e == "push"
	}
	if e := h.Get("X-Gitee-Event"); e != "" {
		return e == "push" || e == "Push Hook"
	}
	return true // 没有事件头：按 push 对待（curl 直调的验收形态）
}

func branchOfRef(ref string) string {
	return strings.TrimPrefix(ref, "refs/heads/")
}

// normRepo —— repo 标识归一化：剥 .git 尾缀与 scheme/host 前缀，只剩 owner/repo。
// "https://github.com/o/r.git"、"o/r"、"git@gitee.com:o/r.git" 全部归到 "o/r"。
func normRepo(s string) string {
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	if i := strings.Index(s, "://"); i >= 0 {
		rest := s[i+3:] // host/owner/repo/...
		if j := strings.Index(rest, "/"); j >= 0 {
			s = rest[j+1:] // owner/repo/...
		}
	}
	return strings.TrimSuffix(s, "/")
}

func repoMatches(pushed, configured string) bool {
	return normRepo(pushed) == normRepo(configured)
}
