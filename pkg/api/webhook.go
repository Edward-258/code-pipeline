package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/engine"
	"pipeline/pkg/repo"
	"pipeline/pkg/webhook"
)

// git webhook 触发（T3.2，对照 Zadig POST /v4/webhook 的极简版）。
// 验签/载荷/匹配的纯逻辑在 pkg/webhook；这里只做 HTTP 编排：
// 验签 → repo+branch 匹配图纸 webhooks 配置 → TriggerWorkflow 正常入队
// （creator=webhook:<repo>@<branch>）。

// Webhook POST /api/webhook/:workflow
func handleWebhook(c *gin.Context) {
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
	sig := webhook.SignatureOf(c.Request.Header)
	if sig == "" {
		// 没带签名头：只在"所有配置都没设 secret"时放行（明确的裸奔模式，不是漏判）
		if !webhook.BareAllowed(wf.Webhooks) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing signature header"})
			return
		}
	} else if !webhook.Valid(sig, body, wf.Webhooks) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "signature mismatch"})
		return
	}

	var p webhook.Payload
	if err := json.Unmarshal(body, &p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad payload: " + err.Error()})
		return
	}
	if !webhook.IsPushEvent(c.Request.Header) {
		c.JSON(http.StatusOK, gin.H{"triggered": false, "reason": "not a push event"})
		return
	}

	if wc, branch := webhook.Pick(wf.Webhooks, p.Ref, p.Repository.FullName); wc != nil {
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
