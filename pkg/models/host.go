package models

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Host —— 远程主机（2026-11-08，对照 Zadig models/ssh_key.go 的极简版）：
// freestyle job 的 spec.hosts 按名字引用它，driver 编译期把私钥投影进 Pod
// （~/.ssh/config + IdentityFile），用户 shell 步骤里直接 `ssh <name> ...`。
// 砍掉的 Zadig 能力：探活、项目授权、VM agent、Windows、产物回传。
//
// 作用域与 workflow/env/registry 一致 = 全局共享（无 RBAC 前提下的既定形态，
// AGENTS §4 决断 #14）。凭证不落任务档案：私钥只在 host 集合与驱动脚本里，
// task 快照只存名字引用——每 attempt 现解析（同 registry 三档时序），retry/clone
// 拿到的是记录的当前值。
//
// 明文姿态与 registry.password / mongo 密码同档（学习版知情接受，仓库已 public）。
type Host struct {
	Name       string    `bson:"_id"         json:"name"`        // 主机名即主键，图纸 spec.hosts 引用它
	UserName   string    `bson:"user_name"   json:"user_name"`   // ssh 登录用户
	IP         string    `bson:"ip"          json:"ip"`          // host[:port] 之外的地址位（IP 或可解析主机名）
	Port       int       `bson:"port"        json:"port"`        // ssh 端口（Validate 归一默认 22）
	PrivateKey string    `bson:"private_key" json:"private_key"` // PEM 私钥全文（OpenSSH/PEM 均可，ssh 认它）
	Desc       string    `bson:"desc,omitempty" json:"desc,omitempty"`
	CreatedAt  time.Time `bson:"created_at"  json:"created_at"`
}

// sshSafeRe —— user/ip 进 ~/.ssh/config 的白名单字符集。config 条目经 heredoc 展开
// （$ 与反引号会执行），IP/user 是入库自由文本，白名单比转义表可靠；IPv6 冒号
// 与 user 的 @ 都在内。
var sshSafeRe = regexp.MustCompile(`^[A-Za-z0-9._@:-]+$`)

// Validate —— 名称形状 / ip·user 非空且字符白名单 / 端口归一默认 22 / 私钥必填
// （没有凭证的"主机记录"什么都连不上，不如建不出来）。
func (h *Host) Validate() error {
	if h.Name == "" {
		return fmt.Errorf("host name is required")
	}
	if !regNameRe.MatchString(h.Name) {
		return fmt.Errorf("host name must match [a-z0-9][a-z0-9-]{0,62}")
	}
	h.IP = strings.TrimSpace(h.IP)
	if h.IP == "" {
		return fmt.Errorf("host ip is required")
	}
	if !sshSafeRe.MatchString(h.IP) {
		return fmt.Errorf("host ip must match [A-Za-z0-9._@:-]+")
	}
	h.UserName = strings.TrimSpace(h.UserName)
	if h.UserName == "" {
		return fmt.Errorf("host user_name is required")
	}
	if !sshSafeRe.MatchString(h.UserName) {
		return fmt.Errorf("host user_name must match [A-Za-z0-9._@:-]+")
	}
	if h.Port == 0 {
		h.Port = 22
	}
	if h.Port < 1 || h.Port > 65535 {
		return fmt.Errorf("host port must be 1-65535")
	}
	if strings.TrimSpace(h.PrivateKey) == "" {
		return fmt.Errorf("host private_key is required")
	}
	return nil
}
