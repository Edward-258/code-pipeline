package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"testing"

	"pipeline/pkg/models"
)

// 纯逻辑单测（handler 级全链路在 pkg/api/webhook_test.go，打真 Mongo）

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestValidAndBareAllowed(t *testing.T) {
	wcs := []*models.WebhookConfig{{Repo: "o/r", Secret: "hush"}}
	body := []byte(`{"ref":"refs/heads/master"}`)
	if !Valid(sign(body, "hush"), body, wcs) {
		t.Error("good signature must pass")
	}
	if Valid(sign(body, "wrong"), body, wcs) {
		t.Error("bad signature must fail")
	}
	if BareAllowed(wcs) {
		t.Error("sealed config must not allow bare requests")
	}
	if !BareAllowed([]*models.WebhookConfig{{Repo: "o/r"}}) {
		t.Error("secret-less config is the explicit bare mode")
	}
}

func TestPickAndNormRepo(t *testing.T) {
	wcs := []*models.WebhookConfig{{Repo: "o/r", Branch: "master"}}
	if wc, branch := Pick(wcs, "refs/heads/master", "https://github.com/o/r.git"); wc == nil || branch != "master" {
		t.Errorf("url-form repo should match: wc=%v branch=%q", wc, branch)
	}
	if wc, _ := Pick(wcs, "refs/heads/dev", "o/r"); wc != nil {
		t.Error("branch mismatch must not pick")
	}
	if normRepo("https://github.com/o/r.git") != "o/r" || normRepo("o/r/") != "o/r" || normRepo("o/r") != "o/r" {
		t.Error("normRepo url/plain forms")
	}
	// 空 Branch = 任意分支
	if wc, _ := Pick([]*models.WebhookConfig{{Repo: "o/r"}}, "refs/heads/whatever", "o/r"); wc == nil {
		t.Error("empty branch is wildcard")
	}
}

func TestIsPushEvent(t *testing.T) {
	gh := httptest.NewRequest("POST", "/x", nil)
	gh.Header.Set("X-GitHub-Event", "ping")
	if IsPushEvent(gh.Header) {
		t.Error("ping is not push")
	}
	gh.Header.Set("X-GitHub-Event", "push")
	if !IsPushEvent(gh.Header) {
		t.Error("github push")
	}
	ge := httptest.NewRequest("POST", "/x", nil)
	ge.Header.Set("X-Gitee-Event", "Push Hook")
	if !IsPushEvent(ge.Header) {
		t.Error("gitee Push Hook is push")
	}
	if !IsPushEvent(httptest.NewRequest("POST", "/x", nil).Header) {
		t.Error("no event header defaults to push (curl acceptance form)")
	}
}
