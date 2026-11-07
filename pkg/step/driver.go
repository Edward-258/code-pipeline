package step

import (
	"fmt"
	"strings"

	"pipeline/pkg/models"
)

// GenerateDriverScript —— 把步骤列表编译成一段 POSIX sh 驱动脚本：我们的 jobexecutor
// （对照 Zadig 在 Pod 里跑 jobexecutor Go 二进制；我们没有二进制，引擎直接代码生成脚本）。
//
// 协议（banner）：
//
//	===MP=== STEP-START <name>
//	===MP=== STEP-END <name> passed|failed exit=<n>
//	===MP=== OUTPUT <k>=<v>          ← 步骤脚本 mp_output "<k>=<v>" 产出（M4 变量传递）
//
// 引擎每 2s 刮 Pod 日志反解 banner 维护步骤状态（engine/logs.go）——日志流就是状态信道，
// 零新依赖零 RBAC。失败即停：对照 jobexecutor 的 hasFailed && !OnFailure（剩余步骤不跑，
// 引擎侧置 skipped），失败退出码透传给 Pod（决断 #3"Pod 退出码 = job 结果"继续成立）。
func GenerateDriverScript(steps []*models.StepSpec, hosts []*models.Host) (string, error) {
	var b strings.Builder
	b.WriteString(driverPrelude)

	// ssh 环境预热（spec.hosts 引用远程主机时）：铺私钥与 ~/.ssh/config，失败即掀翻
	// 整个驱动脚本（hosts.go）——不带病进步骤。放步骤函数定义前：预热属于环境，
	// 不属于任何一步
	setup, err := GenerateHostSetup(hosts)
	if err != nil {
		return "", err
	}
	b.WriteString(setup)

	// 先全部渲染：任一步骤规格非法（如 git 没 repo）在这里就报错，别等 Pod 起来才炸
	for i, s := range steps {
		frag, err := newStepCtl(s).PreRun()
		if err != nil {
			return "", fmt.Errorf("step %q: %w", s.Name, err)
		}
		fmt.Fprintf(&b, "\nmp_step_%d() {\n%s\n}\n", i+1, frag)
	}

	b.WriteString("\n")
	for i, s := range steps {
		fmt.Fprintf(&b, "mp_run '%s' mp_step_%d\n", s.Name, i+1)
	}
	return b.String(), nil
}

// driverPrelude —— 驱动脚本的公共骨架。⚠️ 保持 POSIX sh（alpine 是 ash 不是 bash）：
// 不用 local、不用 [[ ]]、不用数组。$1=步骤名（banner 用），$2=步骤函数名。
//
// 首行 exec 2>&1 是协议的地基（2026-11-04 幕1 首跑踩实）：containerd shim 用两条独立
// copier 追写同一日志文件，写入顺序≠写出时间——stderr 块可能落在任何 banner 的前面
// （实测 gonna-die 落在了 STEP-START 前），按 banner 切分就把整段 stderr 切进黑洞。
// 在 shell 层合并成单流：单 copier = 文件顺序严格等于写出顺序，切分从此确定性成立。
//
// say 的前置 \n 是协议护栏：步骤输出可能没有尾换行（如 printf 无 \n 的 XML），
// 直接 echo 会让 banner 黏在输出尾部、不在行首——解析器就看瞎了。
// EXIT trap 是另一道护栏：步骤体里写裸 `exit N` 时 shell 直接死亡、mp_run 的
// END banner 永远到不了——trap 此刻补发末班 banner，退出码不丢。
// 正常路径先把 MP_CUR 清空再 exit，trap 看到空就哑火，不会双打。
const driverPrelude = `exec 2>&1
say() { printf '\n===MP=== %s\n' "$*"; }
mp_output() { say "OUTPUT $1"; }
MP_CUR=""
mp_run() {
  say "STEP-START $1"
  MP_CUR="$1"
  "$2"
  rc=$?
  MP_CUR=""
  if [ "$rc" -eq 0 ]; then
    say "STEP-END $1 passed exit=0"
  else
    say "STEP-END $1 failed exit=$rc"
    exit "$rc"
  fi
}
trap 'rc=$?; if [ -n "$MP_CUR" ]; then say "STEP-END $MP_CUR failed exit=$rc"; fi' EXIT
`
