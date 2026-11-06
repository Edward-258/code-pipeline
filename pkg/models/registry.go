package models

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// regNameRe —— 仓库名形状（与 K8s namespace 命名同构，同 api/envs.go 的 nameRe；
// models 层放一份：ValidateWorkflow/BuildTask 都要用，不能反向 import api）
var regNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Registry —— 镜像仓库（2026-11-07，对照 Zadig models/registry_namespace.go 的极简版）：
// docker_build 步骤的推送目标，凭证由引擎在快照时烘焙进步骤 spec（数据直通，Pod 内
// docker login——Zadig 同款链路，砍掉云厂商动态换凭证/项目授权/DinD 同步）。
// 明文姿态与 mongo/ACR 密码同档（学习版知情接受，仓库已 public）。
//
// IsDefault 是 Zadig 全量制的核心语义（用户拍板 2026-11-07）：全局唯一默认仓库，
// 建立后所有【裸名】docker_build（无 registry 引用、镜像名不带仓库前缀）跟随它推送；
// 没有默认记录时裸名回落集群内置 registry（$MP_NODE_REGISTRY，行为同 0.7.0 及以前）。
// ⚠️ 设置默认会静默改变所有裸名图纸的推送目标——前端表单与 README 都要写明。
type Registry struct {
	Name      string    `bson:"_id"        json:"name"`    // 仓库名即主键，图纸 registry 字段引用它
	Address   string    `bson:"address"    json:"address"` // host[:port][/namespace]，入库前剥 scheme
	Username  string    `bson:"username,omitempty" json:"username,omitempty"`
	Password  string    `bson:"password,omitempty" json:"password,omitempty"` // 空 = 免认证仓库（登录步骤跳过）
	IsDefault bool      `bson:"is_default,omitempty" json:"is_default,omitempty"`
	Desc      string    `bson:"desc,omitempty" json:"desc,omitempty"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}

// Validate —— address 必填；剥掉 http(s):// 前缀（Zadig GetRegistryAddress 语义：docker
// 只认 host[:port]，scheme 会让 docker login/push 报 invalid reference）
func (r *Registry) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("registry name is required")
	}
	if !regNameRe.MatchString(r.Name) {
		return fmt.Errorf("registry name must match [a-z0-9][a-z0-9-]{0,62}")
	}
	r.Address = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(r.Address), "https://"), "http://")
	if r.Address == "" {
		return fmt.Errorf("registry address is required")
	}
	return nil
}
