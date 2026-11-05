package step

import (
	"fmt"
	"net/url"
	"strings"

	"pipeline/pkg/models"
)

// git 步骤：clone --depth 1 后把 shell 留在仓库目录（后续步骤天然在源码根上跑）。

type gitCtl struct{ spec *models.StepSpec }

func (c *gitCtl) PreRun() (string, error) {
	if c.spec.Repo == "" {
		return "", fmt.Errorf("git step needs repo")
	}
	repo := c.spec.Repo
	if c.spec.Token != "" {
		repo = injectToken(repo, c.spec.Token) // 私有仓库：令牌进 URL，不落 ps 可见的命令行……好吧还是命令行，学习版
	}
	dir := c.spec.ContextDir
	if dir == "" {
		dir = repoDirName(c.spec.Repo) // 未指定目录 = 从 URL 尾段推
	}
	branch := c.spec.Branch
	if branch == "" {
		branch = "main"
	}
	return fmt.Sprintf("rm -rf '%s' && git clone --depth 1 -b '%s' '%s' '%s' && cd '%s'",
		dir, branch, repo, dir, dir), nil
}
func (c *gitCtl) AfterRun(*models.StepTask) error { return nil }

// injectToken —— https://host/o/r(.git) → https://<token>@host/o/r(.git)；
// token 形如 "user:pass" 时用 UserPassword，纯 token 用 User
func injectToken(repo, token string) string {
	u, err := url.Parse(repo)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return repo // 解析不动就原样返回（clone 自己会报错，错误信息足够定位）
	}
	if i := strings.Index(token, ":"); i > 0 {
		u.User = url.UserPassword(token[:i], token[i+1:])
	} else {
		u.User = url.User(token)
	}
	return u.String()
}

// repoDirName —— URL 尾段当目录名（github.com/o/r.git → r），非法字符压成 '-'
func repoDirName(repo string) string {
	s := strings.TrimSuffix(repo, ".git")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	s = unsafeDirChars.ReplaceAllString(s, "-")
	if s == "" {
		s = "repo"
	}
	return s
}
