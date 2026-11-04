package engine

import (
	"fmt"
	"time"

	"pipeline/pkg/models"
)

// 图纸 → 运行实例的构造（原 api 包 buildTask/snapshotWorkflow，T3.1 下沉到 engine：
// cron/webhook 触发器与 API 的 RunWorkflow/Clone 共用同一份"发号 → 快照 → 建树"逻辑，
// 触发器们不许各写各的快照——图纸/实例分离的灵魂只允许一个实现）。

// buildTask —— 发号后的实例构造。⭐灵魂：WorkflowArgs 内嵌【当时的完整图纸快照】，
// 此后改图纸/删图纸都不影响查历史、克隆重跑。
func BuildTask(wf *models.Workflow, taskID int64) *models.WorkflowTask {
	stages := make([]*models.StageTask, 0, len(wf.Stages))
	for _, st := range wf.Stages {
		stt := &models.StageTask{
			Name:     st.Name,
			Parallel: st.Parallel,
			Status:   models.StatusCreated,
		}
		if st.ManualExec != nil {
			// 运行时副本：Executed 归零（M3 放行时在这里翻 true）；
			// T3.4 审批配置随快照走（approvers/policy 是闸门语义的一部分，丢了 all 就变 any——活体验收抓的）
			stt.ManualExec = &models.ManualExec{
				Enabled:   st.ManualExec.Enabled,
				Approvers: append([]string(nil), st.ManualExec.Approvers...),
				Policy:    st.ManualExec.Policy,
			}
		}
		for _, jb := range st.Jobs {
			var specCopy models.FreestyleSpec
			if jb.Spec != nil { // deploy job 无 freestyle spec，跳过
				specCopy = *jb.Spec // 浅拷贝 + steps 逐个深拷贝（快照绝不共享指针）
				if len(jb.Spec.Steps) > 0 {
					specCopy.Steps = make([]*models.StepSpec, len(jb.Spec.Steps))
					for i, ss := range jb.Spec.Steps {
						cp := *ss
						specCopy.Steps[i] = &cp
					}
				}
			}
			var ds *models.DeploySpec
			if jb.DeploySpec != nil {
				cp := *jb.DeploySpec // 值结构浅拷贝即深拷贝（全标量字段）
				ds = &cp
			}
			// 步骤翻译：图纸 steps → 运行时 StepTask（与 EffectiveSteps 同源：旧图纸 Scripts → 隐式 "main"）
			var specPtr *models.FreestyleSpec
			var steps []*models.StepTask
			if jb.Spec != nil {
				specPtr = &specCopy
				eff := specCopy.EffectiveSteps()
				steps = make([]*models.StepTask, len(eff))
				for i, ss := range eff {
					steps[i] = &models.StepTask{Name: ss.Name, Type: ss.Type, Status: models.StatusCreated}
				}
			}
			stt.Jobs = append(stt.Jobs, &models.JobTask{
				Name:       jb.Name,
				JobType:    jb.JobType,
				Status:     models.StatusCreated,
				Spec:       specPtr,
				DeploySpec: ds,
				Steps:      steps,
			})
		}
		stages = append(stages, stt)
	}

	return &models.WorkflowTask{
		ID:            fmt.Sprintf("%s-%d", wf.Name, taskID), // 自然键：插入前定好，ack 全靠它
		TaskID:        taskID,
		WorkflowName:  wf.Name,
		Status:        models.StatusCreated,
		Creator:       "api",
		WorkflowArgs:  SnapshotWorkflow(wf),
		GlobalContext: copyStrings(wf.GlobalContext), // M4：图纸级变量 → 任务初始上下文（快照独立副本，改图纸不影响在跑任务）
		Stages:        stages,
		CreatedAt:     time.Now(),
	}
}

// copyStrings —— string map 浅值深拷贝（值是 string，拷 map 结构即可）
func copyStrings(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// snapshotWorkflow —— 图纸深拷贝（Stage/Job/Spec 逐层复制，绝不共享指针）
func SnapshotWorkflow(wf *models.Workflow) *models.Workflow {
	out := &models.Workflow{
		Name:          wf.Name,
		Stages:        make([]*models.Stage, 0, len(wf.Stages)),
		GlobalContext: copyStrings(wf.GlobalContext), // 快照连变量初始值一起封存
		CreatedAt:     wf.CreatedAt,
		UpdatedAt:     wf.UpdatedAt,
	}
	for _, st := range wf.Stages {
		s := &models.Stage{Name: st.Name, Parallel: st.Parallel}
		if st.ManualExec != nil {
			me := *st.ManualExec
			me.Approvals = nil // 审批记录属于某次运行，图纸快照不带
			me.Executed = false
			s.ManualExec = &me
		}
		for _, jb := range st.Jobs {
			var specCopy models.FreestyleSpec
			if jb.Spec != nil { // deploy job 无 freestyle spec，跳过
				specCopy = *jb.Spec
				if len(jb.Spec.Steps) > 0 {
					specCopy.Steps = make([]*models.StepSpec, len(jb.Spec.Steps))
					for i, ss := range jb.Spec.Steps {
						cp := *ss
						specCopy.Steps[i] = &cp
					}
				}
			}
			var ds *models.DeploySpec
			if jb.DeploySpec != nil {
				cp := *jb.DeploySpec
				ds = &cp
			}
			var specPtr *models.FreestyleSpec
			if jb.Spec != nil {
				specPtr = &specCopy
			}
			s.Jobs = append(s.Jobs, &models.Job{Name: jb.Name, JobType: jb.JobType, Spec: specPtr, DeploySpec: ds})
		}
		out.Stages = append(out.Stages, s)
	}
	return out
}
