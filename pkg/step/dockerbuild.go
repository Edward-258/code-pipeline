package step

import (
	"fmt"
	"strings"

	"pipeline/pkg/models"
)

// docker_build 步骤：遥控宿主 daemon 构建 + 推镜像仓库。
// 推送目标三档（解析在 engine/exec/registry.go，烘焙成 RegAddr/RegUser/RegPass 副本）：
//   RegAddr != "" → 仓库引用/默认跟随档：全名 RegAddr/Image:tag，有凭证先 docker login
//   其余          → 内置本地档（0.7.x 原路径）：裸名补 $MP_NODE_REGISTRY/，全名原样直推

type dockerBuildCtl struct{ spec *models.StepSpec }

func (c *dockerBuildCtl) PreRun() (string, error) {
	if c.spec.Image == "" {
		return "", fmt.Errorf("docker_build step needs image")
	}
	tag := c.spec.Tag
	if tag == "" {
		tag = "latest"
	}
	df := c.spec.Dockerfile
	if df == "" {
		df = "Dockerfile"
	}
	ctx := c.spec.ContextDir
	if ctx == "" {
		ctx = "."
	}

	if c.spec.RegAddr != "" {
		// 仓库引用档：镜像名 = addr/相对名:tag——引用模式下 image 就是仓库相对路径
		// （与 Zadig RegAddr[/Namespace]/imageTag 的拼法同构，aslan 也是在任务侧拼好全名）
		full := fmt.Sprintf("%s/%s:%s", c.spec.RegAddr, c.spec.Image, tag)
		cmd := fmt.Sprintf("docker build -f %s -t %s %s && docker push %s",
			shQuote(df), shQuote(full), shQuote(ctx), shQuote(full))
		if c.spec.RegUser != "" {
			// 凭证走 stdin（-p 会进进程列表）+ DOCKER_CONFIG 指到 /tmp：/root/.docker hostPath
			// 是只读挂载（build.go），login 写不进去——凭证也不该写回节点。子 shell 包裹，
			// export 不外泄到同 shell 的后续步骤。值经 shQuote 转义（凭证里什么字符都可能有）
			cmd = fmt.Sprintf("( export DOCKER_CONFIG=/tmp/.docker && printf '%%s' %s | docker login -u %s --password-stdin %s && %s )",
				shQuote(c.spec.RegPass), shQuote(c.spec.RegUser), shQuote(c.spec.RegAddr), cmd)
		}
		return cmd, nil
	}

	// 内置本地档
	name := c.spec.Image
	prefix := ""
	if !models.HasRegistryPrefix(name) {
		// 决断 #3：产物进集群 registry:2，kubelet 走 localhost:5000 mirror 拉取。
		// ⚠️ 前缀段必须放单引号外用双引号包住——sh 不展开单引号里的变量，
		// 整段单引号会让 docker 收到字面量 "$MP_NODE_REGISTRY/..."（exit=125 invalid reference，
		// 幕1 首跑实录）。相邻引号段拼接是一个词："$MP_NODE_REGISTRY"/'mini-demo':'0.1.0'。
		prefix = `"$MP_NODE_REGISTRY"/`
	}
	return fmt.Sprintf("docker build -f '%s' -t %s'%s:%s' '%s' && docker push %s'%s:%s'",
		df, prefix, name, tag, ctx, prefix, name, tag), nil
}
func (c *dockerBuildCtl) AfterRun(*models.StepTask) error { return nil }

// shQuote —— POSIX 单引号安全包装（值内的 ' 写成 '\”）：凭证字段（password 里什么字符都可能有）
// 不转义会掀翻驱动脚本。仓库地址/用户名同待遇。
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
