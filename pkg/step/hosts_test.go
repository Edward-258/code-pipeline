package step

import (
	"encoding/base64"
	"os/exec"
	"strings"
	"testing"

	"pipeline/pkg/models"
)

// TestGenerateHostSetup —— ssh 环境预热段编译：base64 内嵌、0600、config 条目、
// 环境变量导出（名字大写 + '-'→'_'）、失败传播。只做字符串断言 + sh -n 语法门——
// ⚠️ 绝不真执行 mp_host_setup：它会写执行者的 $HOME/.ssh/config（覆盖真配置）。
func TestGenerateHostSetup(t *testing.T) {
	key := "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----\n"
	hosts := []*models.Host{
		{Name: "build-node", UserName: "root", IP: "172.29.237.250", Port: 22, PrivateKey: key},
		{Name: "3legacy", UserName: "deploy", IP: "10.0.0.3", Port: 2222, PrivateKey: key},
	}
	out, err := GenerateHostSetup(hosts)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// 私钥 base64 单行内嵌（字母表无引号，不破 mp_run 的单引号协议）且解码回原钥
	b64 := base64.StdEncoding.EncodeToString([]byte(key))
	if !strings.Contains(out, "printf '%s' '"+b64+"' | base64 -d > /tmp/mp_host_1_PK") {
		t.Errorf("key not embedded as base64 one-liner")
	}
	if !strings.Contains(out, "chmod 600 /tmp/mp_host_1_PK") {
		t.Errorf("key file must be chmod 600")
	}

	// config 条目：别名=记录名，accept-new 首连收键
	for _, want := range []string{
		"Host build-node", "HostName 172.29.237.250", "User root", "Port 22",
		"IdentityFile /tmp/mp_host_1_PK", "StrictHostKeyChecking accept-new",
		"Port 2222",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("ssh config missing %q", want)
		}
	}

	// 环境变量导出：大写 + '-'→'_'；首字符为数字的名字也要合法（MP_H_ 前缀兜底）
	for _, want := range []string{
		"MP_H_BUILD_NODE_IP='172.29.237.250'", "MP_H_BUILD_NODE_PORT='22'", "MP_H_BUILD_NODE_USER='root'",
		"MP_H_3LEGACY_PORT='2222'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("env export missing %q", want)
		}
	}

	// 失败传播：函数包壳 + 掀翻驱动脚本
	if !strings.Contains(out, "mp_host_setup || exit $?") {
		t.Errorf("setup failure must abort the driver before any step")
	}

	// sh -n 语法门（只解析不执行——执行会污染真实 $HOME）
	if err := exec.Command("sh", "-n", "-c", out).Run(); err != nil {
		t.Fatalf("setup snippet is not valid POSIX sh: %v", err)
	}
}

// TestGenerateHostSetupEmpty —— 无主机 = 零片段（纯 shell 老图纸的驱动脚本一字节不变）
func TestGenerateHostSetupEmpty(t *testing.T) {
	out, err := GenerateHostSetup(nil)
	if err != nil || out != "" {
		t.Fatalf("empty hosts must yield empty snippet, got %q, %v", out, err)
	}
}

// TestGenerateDriverScriptWithHosts —— 签名扩展接线：预热段在 prelude 之后、
// 第一个步骤函数之前（先铺环境再定义步骤）。
func TestGenerateDriverScriptWithHosts(t *testing.T) {
	steps := []*models.StepSpec{{Name: "s", Type: models.StepShell, Script: "echo hi"}}
	hosts := []*models.Host{{Name: "x", UserName: "root", IP: "10.0.0.1", Port: 22, PrivateKey: "k"}}
	script, err := GenerateDriverScript(steps, hosts)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	prelude := strings.Index(script, "mp_run() {")
	setup := strings.Index(script, "mp_host_setup()")
	firstStep := strings.Index(script, "mp_step_1() {")
	if prelude < 0 || setup < prelude || firstStep < setup {
		t.Fatalf("order broken: prelude=%d setup=%d firstStep=%d", prelude, setup, firstStep)
	}
}
