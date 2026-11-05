package step

import (
	"fmt"
	"strings"

	"pipeline/pkg/models"
)

// docker_build 步骤：遥控宿主 daemon 构建 + 推集群内 registry:2（决断 #5/#3）。

type dockerBuildCtl struct{ spec *models.StepSpec }

func (c *dockerBuildCtl) PreRun() (string, error) {
	if c.spec.Image == "" {
		return "", fmt.Errorf("docker_build step needs image")
	}
	name := c.spec.Image
	prefix := ""
	if !hasRegistryPrefix(name) {
		// 决断 #3：产物进集群 registry:2，kubelet 走 localhost:5000 mirror 拉取。
		// ⚠️ 前缀段必须放单引号外用双引号包住——sh 不展开单引号里的变量，
		// 整段单引号会让 docker 收到字面量 "$MP_NODE_REGISTRY/..."（exit=125 invalid reference，
		// 幕1 首跑实录）。相邻引号段拼接是一个词："$MP_NODE_REGISTRY"/'mini-demo':'0.1.0'。
		prefix = `"$MP_NODE_REGISTRY"/`
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
	return fmt.Sprintf("docker build -f '%s' -t %s'%s:%s' '%s' && docker push %s'%s:%s'",
		df, prefix, name, tag, ctx, prefix, name, tag), nil
}
func (c *dockerBuildCtl) AfterRun(*models.StepTask) error { return nil }

// hasRegistryPrefix —— 首段含 '.'/':' 或就是 localhost 视为已带仓库前缀
// （"mini-demo" → 要加 $MP_NODE_REGISTRY/ 前缀；"172.29.x.x:3050/foo" → 原样）
func hasRegistryPrefix(image string) bool {
	first := image
	if i := strings.Index(image, "/"); i >= 0 {
		first = image[:i]
	}
	return first == "localhost" || strings.ContainsAny(first, ".:")
}
