package engine

import (
	"fmt"
	"log"
	"sync"
	"time"

	"pipeline/pkg/engine/cancel"
	"pipeline/pkg/models"
)

// stage 级状态机（对照 Zadig workflowcontroller/stage.go）：stage 严格按序，
// 跑完聚合，失败刹车；闸门（durable 暂停）与取消检查点都在 stage 边界。

// runStages —— 对照 Zadig RunStages：stage 严格按序，跑完聚合，失败刹车。
//   - 跳过 passed/skipped 的 stage（重跑恢复的入口，M3 暂停恢复全靠它）
//   - stage 终态 = 其 jobs 的最差者（WorstOf）
//   - stage 终态命中 StatusStopped → 后续 stage 全部 skipped，任务收口
func runStages(tc *taskCtx, t *models.WorkflowTask) {
	for _, st := range t.Stages {
		if st.Status == models.StatusPassed || st.Status == models.StatusSkipped {
			continue
		}

		// T1.5 取消检查点（stage 边界）：取消信号在 stage 间隙到的，当场收口——
		// 本 stage 置 skipped，后续 stage 一并 skipped，任务 cancelled
		if cancel.Signalled(tc.cancelCh) {
			st.Status = models.StatusSkipped
			markRestSkipped(t, st)
			finish(t, models.StatusCancelled, "cancelled by user")
			log.Printf("engine: task %s/%d cancelled at stage %q boundary", t.WorkflowName, t.TaskID, st.Name)
			return
		}

		// M3 闸门（对照 Zadig stage.go waitForManualExec）：stage 开跑前若启用手动执行且未放行
		// → stage/task 置 pause 落库 → goroutine 直接 return（不 sleep 不占线程）。
		// durable 的全部秘密：此刻起世上只剩 Mongo 里这份 pause 档案，进程死活与它无关；
		// release 端点标记 Executed=true 后重新 Run，凭上面的跳过逻辑从暂停的 stage 接着跑。
		if stageGated(st) {
			st.Status = models.StatusPause
			if st.ManualExec != nil {
				st.ManualExec.NotificationSent = true // 对照 Zadig 通知去重位；学习版只置位不发送
			}
			t.Status = models.StatusPause
			tc.ack(t)
			log.Printf("engine: task %s/%d paused at stage %q (waiting release)", t.WorkflowName, t.TaskID, st.Name)
			return // ⭐ 不走 finish：任务没结束，只是暂停
		}

		st.Status = models.StatusRunning
		st.StartTime = time.Now().Unix()
		tc.ack(t)
		log.Printf("engine: task %s/%d stage %q running", t.WorkflowName, t.TaskID, st.Name)

		runStage(tc, t, st)

		st.Status = jobsWorst(st)
		if st.Status == models.StatusFailed && st.Error == "" {
			for _, jt := range st.Jobs { // 把第一个失败 job 的错误冒泡到 stage 层
				if jt.Status == models.StatusFailed {
					st.Error = fmt.Sprintf("job %q failed: %s", jt.Name, jt.Error)
					break
				}
			}
		}
		st.EndTime = time.Now().Unix()
		tc.ack(t)
		log.Printf("engine: task %s/%d stage %q %s", t.WorkflowName, t.TaskID, st.Name, st.Status)

		// T1.9：stage 级刹车只认"硬失败"——全靠 ignore 策略失败堆出来的 failed stage
		// 不刹车，后续 stage 照跑（控制/上报分离：状态如实红，控制流放行）
		if models.StatusStopped(st.Status) && stageHasHardFailure(st) {
			markRestSkipped(t, st)
			finish(t, st.Status, fmt.Sprintf("stage %q %s: %s", st.Name, st.Status, st.Error))
			log.Printf("engine: task %s/%d %s (braked at stage %q)", t.WorkflowName, t.TaskID, st.Status, st.Name)
			return
		}
	}

	// 尾态不再硬编码 passed：按各 stage 的最差者胜如实收口
	// （全 passed → passed；含 ignore 失败 → failed，但流水线跑到了终点）
	statuses := make([]models.Status, 0, len(t.Stages))
	for _, st := range t.Stages {
		statuses = append(statuses, st.Status)
	}
	finish(t, models.WorstOf(statuses...), "")
	log.Printf("engine: task %s/%d %s", t.WorkflowName, t.TaskID, t.Status)
}

// runStage —— stage 内执行策略（对照 Zadig stage.go runStage → CustomStageCtl.Run）：
//   - Parallel=true：jobs 全员齐飞（goroutine + WaitGroup），等全部落地再聚合
//   - Parallel=false：逐个放行；任一 job 命中刹车态 → 剩余未启动的直接 skipped
//
// 刹车语义：不再起新的，已在跑的让它跑完（并行模式天然如此：WaitGroup 等全员）。
func runStage(tc *taskCtx, t *models.WorkflowTask, st *models.StageTask) {
	if st.Parallel {
		var wg sync.WaitGroup
		for _, jt := range st.Jobs {
			if jt.Status == models.StatusPassed {
				continue // T1.8 整任务重试：passed job 原样保留不重跑（retried stage 里混着 passed 子树）
			}
			wg.Add(1)
			go func(jt *models.JobTask) {
				defer wg.Done()
				runJobTask(tc, t, st, jt)
			}(jt)
		}
		wg.Wait()
		return
	}

	for _, jt := range st.Jobs {
		if jt.Status == models.StatusPassed {
			continue // 同上：重试场景下 passed job 不重跑
		}
		if stageBraked(st) {
			jt.Status = models.StatusSkipped // 不再起新的：未启动的直接置 skipped
			tc.ack(t)
			continue
		}
		runJobTask(tc, t, st, jt)
	}
}

// stageGated —— M3 闸门判断（对照 Zadig waitForManualExec 的三个前置）：
// 启用了手动执行、且人还没放行过（Executed 由 release 端点翻 true）
func stageGated(st *models.StageTask) bool {
	return st.ManualExec != nil && st.ManualExec.Enabled && !st.ManualExec.Executed
}

// stageBraked —— 串行模式下刹车判断：stage 内存在"硬失败"的 job。
// T1.9：ignore 策略的失败 job 不算硬失败——记红但继续下一个 job。
func stageBraked(st *models.StageTask) bool {
	for _, jt := range st.Jobs {
		if jt.Status == models.StatusFailed && !isIgnorePolicy(jt) {
			return true
		}
		if jt.Status == models.StatusCancelled { // 取消是最高指令，任何策略都刹
			return true
		}
	}
	return false
}

// stageHasHardFailure —— stage 级刹车依据（对照 stageBraked 的 stage 间版本）
func stageHasHardFailure(st *models.StageTask) bool {
	for _, jt := range st.Jobs {
		if (jt.Status == models.StatusFailed && !isIgnorePolicy(jt)) || jt.Status == models.StatusCancelled {
			return true
		}
	}
	return false
}

// isIgnorePolicy —— 该 job 的失败是否走 ignore 放行
func isIgnorePolicy(jt *models.JobTask) bool {
	return jt.Spec != nil && jt.Spec.ErrorPolicy == models.ErrorPolicyIgnore
}
