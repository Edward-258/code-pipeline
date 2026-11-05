package step

import (
	"fmt"
	"strings"
	"time"

	"pipeline/pkg/models"
)

// 日志流 = 状态信道（见 driver.go 的 banner 协议）。这里全是纯函数，
// 表测友好；持锁纪律由调用方（engine/exec）负责。Pod 日志 I/O 在 pkg/k8s。

const (
	bannerPrefix = "===MP==="
	maxStepLog   = 64 * 1024 // 每步日志存档上限（保尾部：报错总在最后）
)

// ApplyStepBanners —— 全量重放 Pod 日志里的 banner（幂等，每 2s 重放一次代价可忽略）：
// STEP-START → running（首次记 StartTime）；STEP-END → passed/failed（记 EndTime）。
// 返回是否有变化（没变就不值得 ack）。未知步骤名/畸形行静默跳过——协议解析不许 panic 引擎。
func ApplyStepBanners(jt *models.JobTask, logText string, now time.Time) bool {
	changed := false
	for _, raw := range strings.Split(logText, "\n") {
		// banner 不保证在行首：上游步骤可能输出无尾换行的内容（printf 的 XML 等），
		// 老驱动会把 banner 黏在其尾部——从行内第一个 banner 前缀处切开再解析
		i := strings.Index(raw, bannerPrefix)
		if i < 0 {
			continue
		}
		payload := strings.TrimSpace(raw[i+len(bannerPrefix):])

		if name, ok := strings.CutPrefix(payload, "STEP-START "); ok {
			if st := findStep(jt, strings.TrimSpace(name)); st != nil && st.Status == models.StatusCreated {
				st.Status = models.StatusRunning
				st.StartTime = now.Unix()
				changed = true
			}
			continue
		}
		// OUTPUT <k>=<v>（M4）：步骤脚本经 mp_output / 裸 echo 产出的变量声明。
		// job 级收割进 jt.Outputs（单行 k=v；值含 '=' 时首个 '=' 后全算值）。
		// 幂等：重放同值不触发 ack；畸形行（无 '='/空键）静默跳过——协议解析不许 panic 引擎。
		// ⚠️ 判变必须用 exists：absent key 读出来也是零值空串，`m[k] != v` 会把
		// "OUTPUT K="（空值首见）误判成无变化、永不落 map（表测抓的真 bug）。
		if rest, ok := strings.CutPrefix(payload, "OUTPUT "); ok {
			if k, v, eq := strings.Cut(rest, "="); eq && strings.TrimSpace(k) != "" {
				if jt.Outputs == nil {
					jt.Outputs = map[string]string{}
				}
				old, exists := jt.Outputs[k]
				if !exists || old != v {
					jt.Outputs[k] = v
					changed = true
				}
			}
			continue
		}
		if rest, ok := strings.CutPrefix(payload, "STEP-END "); ok {
			// 末段 exit=N、次末段 verdict、其余全是 name（名字允许含空格，从右往左剥）
			f := strings.Fields(rest)
			if len(f) < 3 || !strings.HasPrefix(f[len(f)-1], "exit=") {
				continue
			}
			verdict, name := f[len(f)-2], strings.Join(f[:len(f)-2], " ")
			st := findStep(jt, name)
			if st == nil || st.Status != models.StatusRunning { // 只从 running 落终态（幂等）
				continue
			}
			st.EndTime = now.Unix()
			if verdict == "passed" {
				st.Status = models.StatusPassed
			} else {
				st.Status = models.StatusFailed
				st.Error = f[len(f)-1] // exit=N，细节在日志里
			}
			changed = true
		}
	}
	return changed
}

// SliceStepLogs —— 按 banner 把全量日志切成每步一段（步骤的日志 = 从它的 STEP-START 行
// 到下一个 STEP-START 行之前的全部输出，含自己的 END banner）。终态时调用一次。
func SliceStepLogs(jt *models.JobTask, fullLog string) {
	var cur *models.StepTask
	var buf strings.Builder
	flush := func() {
		if cur != nil {
			cur.Log = buf.String()
		}
	}
	for _, line := range strings.Split(fullLog, "\n") {
		if payload, ok := trimBanner(line, "STEP-START "); ok {
			flush()
			cur = findStep(jt, strings.TrimSpace(payload)) // 未知步骤：切到黑洞
			buf.Reset()
			if cur != nil {
				buf.WriteString(line + "\n")
			}
			continue
		}
		if cur != nil {
			buf.WriteString(line + "\n")
		}
	}
	flush()
}

// FinalizeSteps —— Job 终态后的步骤收口，返回步骤级失败摘要（"" = 无失败步骤）：
//  1. created（没轮到跑，失败即停/刹车）→ skipped；running（有 START 无 END，Pod 暴死）→ failed
//  2. 逐步骤 AfterRun——junit 门禁在这里把 passed 翻成 failed（必须在截断前，要读全量日志）
//  3. 日志截断 64KB 保尾部，落档案
func FinalizeSteps(jt *models.JobTask) string {
	for _, st := range jt.Steps {
		switch st.Status {
		case models.StatusCreated:
			st.Status = models.StatusSkipped
		case models.StatusRunning:
			st.Status = models.StatusFailed
			st.Error = "step did not finish (no STEP-END banner; pod died or was killed)"
		}
	}

	stepErr := ""
	specs := map[string]*models.StepSpec{}
	if jt.Spec != nil {
		for _, sp := range jt.Spec.EffectiveSteps() {
			specs[sp.Name] = sp
		}
	}
	for _, st := range jt.Steps {
		if sp, ok := specs[st.Name]; ok {
			_ = newStepCtl(sp).AfterRun(st) // 门禁失败写进 st.Status/Error，不向上抛
		}
		if st.Status == models.StatusFailed && stepErr == "" {
			stepErr = fmt.Sprintf("step %q failed: %s", st.Name, st.Error)
		}
	}

	for _, st := range jt.Steps {
		if len(st.Log) > maxStepLog {
			st.Log = "\n...[log truncated, tail shown]...\n" + st.Log[len(st.Log)-maxStepLog:]
		}
	}
	return stepErr
}

// ---------- 小工具（包内共享） ----------

func findStep(jt *models.JobTask, name string) *models.StepTask {
	for _, st := range jt.Steps {
		if st.Name == name {
			return st
		}
	}
	return nil
}

func trimBanner(line, marker string) (string, bool) {
	i := strings.Index(line, bannerPrefix) // 行中 banner 同样认（与 ApplyStepBanners 同一护栏）
	if i < 0 {
		return "", false
	}
	return strings.CutPrefix(strings.TrimSpace(line[i+len(bannerPrefix):]), marker)
}
