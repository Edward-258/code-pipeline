package step

import (
	"encoding/base64"
	"fmt"
	"strings"

	"pipeline/pkg/models"
)

// GenerateHostSetup —— 把解析好的主机记录（exec.resolveHosts 的产物）编译成驱动脚本的
// ssh 环境预热段（2026-11-08，对照 Zadig jobexecutor 对 build.SSHs 的 Pod 内投影）：
//
//   - 私钥 base64 内嵌 → base64 -d 落 /tmp/mp_host_<n>_PK（0600）——私钥是多行 PEM，
//     直接拼进 POSIX 脚本会被换行/引号撕碎，base64 单行字母表无引号，是唯一稳态
//   - ~/.ssh/config 条目（Host 别名 = 记录名，StrictHostKeyChecking accept-new 首连自动收键）
//   - 导出 MP_H_<NAME>_IP/_PORT/_USER（名字大写、'-'→'_'；MP_H_ 前缀保证首字符为数字的
//     名字也合法，并归入 MP_ 环境变量命名空间），用户脚本里 `echo $MP_H_X_IP` 即取
//
// 失败传播：整段包成 mp_host_setup 函数，`|| exit $?`——私钥写不进/目录建不出在
// 任何步骤开始前掀翻驱动脚本（job 红灯），不带病进步骤。
func GenerateHostSetup(hosts []*models.Host) (string, error) {
	if len(hosts) == 0 {
		return "", nil
	}
	var b strings.Builder
	b.WriteString(`mp_host_setup() {
  MP_SSH_HOME="${HOME:-/root}"
  mkdir -p "$MP_SSH_HOME/.ssh" || return 1
  chmod 700 "$MP_SSH_HOME/.ssh"
  : > "$MP_SSH_HOME/.ssh/config"
`)
	for i, h := range hosts {
		n := i + 1
		keyFile := fmt.Sprintf("/tmp/mp_host_%d_PK", n)
		b64 := base64.StdEncoding.EncodeToString([]byte(h.PrivateKey))
		fmt.Fprintf(&b, "\n  # host %d: %s = %s@%s:%d\n", n, h.Name, h.UserName, h.IP, h.Port)
		fmt.Fprintf(&b, "  printf '%%s' '%s' | base64 -d > %s || return 1\n", b64, keyFile)
		fmt.Fprintf(&b, "  chmod 600 %s\n", keyFile)
		// config 条目走 heredoc：内容按记录原样展开（$ 与反引号会被 shell 展开——
		// ip/user 在 models.Host.Validate 已过字符白名单，注入面封死）
		fmt.Fprintf(&b, `  cat >> "$MP_SSH_HOME/.ssh/config" <<MP_HOST_EOF
Host %s
    HostName %s
    User %s
    Port %d
    IdentityFile %s
    StrictHostKeyChecking accept-new
MP_HOST_EOF
`, h.Name, h.IP, h.UserName, h.Port, keyFile)
		v := hostEnvName(h.Name)
		fmt.Fprintf(&b, "  %s_IP='%s' %s_PORT='%d' %s_USER='%s'\n", v, h.IP, v, h.Port, v, h.UserName)
		fmt.Fprintf(&b, "  export %s_IP %s_PORT %s_USER\n", v, v, v)
	}
	b.WriteString("}\nmp_host_setup || exit $?\n")
	return b.String(), nil
}

// hostEnvName —— 主机名 → 环境变量名段：MP_H_ 前缀（首字符为数字的名字也合法，
// 并归入 MP_ 命名空间）+ 大写 + '-'→'_'（shell 标识符不允许 '-'）
func hostEnvName(name string) string {
	return "MP_H_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}
