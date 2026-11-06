package exec

import (
	"fmt"

	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// docker_build 推送目标解析（2026-11-07 registry 管理，Zadig 全量制语义）。
// 解析发生在 driver 编译前（runK8sJobOnce 每 attempt 一次）而非快照时——与 deploy
// job 的 envResolver 同一时序：凭证不落任务档案（任务 API 不泄明文），retry/clone
// 永远拿到仓库记录的当前值（改密码后重试可用）。烘焙进驱动脚本的副本，任务树不动。
//
// 三档优先级（README/AGENTS 同款规则表）：
//  1. 显式 registry 引用   → 解析该记录（不存在 = job 失败，错误信息点名）
//  2. 裸名 + 全局默认存在  → 跟随 is_default 记录（⚠️ 设默认会改变所有裸名图纸的目标）
//  3. 其余                 → 原样直通（全名走 /root/.docker 兜底；裸名回落 $MP_NODE_REGISTRY）
var (
	registryResolver      = repo.FindRegistry        // var 钩子：单测 stub（同 deploy.go envResolver 模式）
	defaultRegistryFinder = repo.FindDefaultRegistry // nil,nil = 无默认记录（合法稳态，不是错误）
)

// resolveRegistrySteps —— 返回一份可烘焙的步骤副本；无解析需求的步骤原指针直通（零拷贝）
func resolveRegistrySteps(steps []*models.StepSpec) ([]*models.StepSpec, error) {
	out := make([]*models.StepSpec, len(steps))
	for i, ss := range steps {
		if ss.Type != models.StepDockerBuild {
			out[i] = ss
			continue
		}
		var reg *models.Registry
		var err error
		switch {
		case ss.Registry != "": // 档 1：显式引用
			reg, err = registryResolver(ss.Registry)
			if err != nil {
				return nil, fmt.Errorf("step %q: resolve registry %q: %w", ss.Name, ss.Registry, err)
			}
		case !models.HasRegistryPrefix(ss.Image): // 档 2：裸名跟随默认（无默认走档 3）
			if reg, err = defaultRegistryFinder(); err != nil {
				return nil, fmt.Errorf("step %q: find default registry: %w", ss.Name, err)
			}
		}
		if reg == nil { // 档 3：原样直通
			out[i] = ss
			continue
		}
		cp := *ss // 浅拷贝即深拷贝（烘焙字段全标量）
		cp.RegAddr, cp.RegUser, cp.RegPass = reg.Address, reg.Username, reg.Password
		out[i] = &cp
	}
	return out, nil
}
