package step

import (
	"fmt"

	"pipeline/pkg/models"
)

// archive 步骤：打包产物 → curl 上传引擎 GridFS（T2.4；下载走 API 的 artifact 端点）。

type archiveCtl struct{ spec *models.StepSpec }

// PreRun —— tar czf 工作区产物 → --data-binary POST 引擎 artifact 端点。
// 上传走日志流之外的第二信道（HTTP），产物不进日志（64KB 截断装不下二进制）。
// Zadig 的三种归档步骤（step_archive/tar_archive/html_report）在 S3 语义上等价，
// 学习版砍 S3 用引擎 GridFS，三种合一种。
func (c *archiveCtl) PreRun() (string, error) {
	if c.spec.ArtifactGlob == "" {
		return "", fmt.Errorf("archive step needs artifact_glob")
	}
	name := c.spec.ArtifactName
	if name == "" {
		name = c.spec.Name + ".tgz"
	}
	name = unsafeDirChars.ReplaceAllString(name, "-") // 进 URL 路径段：白名单消毒（$ 等在双引号里照样会被 shell 展开）
	prefix := ""
	if d := c.spec.ContextDir; d != "" {
		prefix = fmt.Sprintf("cd '%s' && ", d)
	}
	// glob 不命中：busybox tar 报 can't find → 步骤失败（产物缺席就该红，不该静默空包）
	// 上传用 curl --data-binary（0.6.x 活体抓的：busybox wget --post-file 在引擎响应到达时
	// 提前断开，POST body 截成 0~41 字节还回 200——静默腐败；archive 步骤的 job 因此落
	// runner 镜像（Spec.NeedsRunner），curl 在 runner 里）。-f：非 2xx 直接非零退出，步骤翻红
	// URL 整体双引号：$ 变量在双引号内照常展开（docker_build 前缀的分段拼接是单引号场景的特例，这里用不上）
	return prefix + fmt.Sprintf(
		"tar czf /tmp/mp-artifact.tgz %s && curl -s -f -X POST -H 'Content-Type: application/gzip' --data-binary @/tmp/mp-artifact.tgz \"$MP_ENGINE_URL/api/tasks/$MP_TASK_WF/$MP_TASK_ID/artifact/%s\" && rm -f /tmp/mp-artifact.tgz",
		c.spec.ArtifactGlob, name), nil
}

func (c *archiveCtl) AfterRun(*models.StepTask) error { return nil }
