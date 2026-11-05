// Package webhook —— git webhook 纯逻辑层：HMAC-SHA256 验签、载荷解析、repo/branch
// 匹配。无 HTTP 依赖（gin 编排在 api）。
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"pipeline/pkg/models"
)

// git webhook 纯逻辑层（T3.2，原 api/webhook.go 的验签/载荷/匹配半边随模块化迁入；
// handler 留在 pkg/api 调 TriggerWorkflow）。只做 GitHub / Gitee 的 push 事件：
// HMAC-SHA256 验签 → repo+branch 匹配图纸 webhooks 配置。
// Gitee 双轨：X-Gitee-Signature（HMAC hex，同 GitHub 算法）或 X-Gitee-Token（明文比对）。

// Payload —— 两个平台的 push 事件里我们只看的字段（hand-pick，容错缺字段）
type Payload struct {
	Ref        string `json:"ref"` // refs/heads/<branch>
	Repository struct {
		FullName   string `json:"full_name"`
		HTMLURL    string `json:"html_url"`
		GitHTTPURL string `json:"git_http_url"`
		GitSSHURL  string `json:"git_ssh_url"`
		CloneURL   string `json:"clone_url"`
	} `json:"repository"`
}

// SignatureOf —— 三种签名头按平台取值（GitHub / Gitee-HMAC / Gitee-Token）
func SignatureOf(h http.Header) string {
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

// BareAllowed —— 没带签名头时的放行判定：只在"所有配置都没设 secret"时放行
// （明确的裸奔模式，不是漏判）
func BareAllowed(wcs []*models.WebhookConfig) bool {
	for _, wc := range wcs {
		if wc.Secret != "" {
			return false
		}
	}
	return true
}

// Valid —— HMAC-SHA256 hex（前缀 sha256= 可有可无）或 Gitee 明文 token 比对，
// 全部走 constant-time 比较；任一配置的 secret 验过即放行（一条图纸可挂多仓库多 secret）
func Valid(sig string, body []byte, wcs []*models.WebhookConfig) bool {
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

func IsPushEvent(h http.Header) bool {
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

// Pick —— branch+repo 双匹配：返回第一个命中的配置（调用方拿它的 Repo 拼 creator）。
// wc.Branch 为空 = 任意分支（宽松模式）；pushedRepo 为空 = 跳过 repo 校验。
func Pick(wcs []*models.WebhookConfig, ref, pushedRepo string) (*models.WebhookConfig, string) {
	branch := branchOfRef(ref)
	for _, wc := range wcs {
		if branch != wc.Branch && wc.Branch != "" {
			continue // 分支不匹配
		}
		if pushedRepo != "" && !repoMatches(pushedRepo, wc.Repo) {
			continue
		}
		return wc, branch
	}
	return nil, branch
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
