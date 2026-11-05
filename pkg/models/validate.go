package models

import "fmt"

// 图纸校验（对照 Zadig workflow.Validate 的极简版）。领域规则归领域模型：
// api 层只做形状绑定（ShouldBindJSON），这里的错一律 400。

// ValidateWorkflow —— 图纸写入（POST/PUT）前的完整性校验：
// 名字/stage/job 结构齐全、job 类型白名单（空 type 视作 freestyle 兼容历史图纸）、
// deploy/freestyle 各自的 spec 必备字段、错误策略白名单、步骤类型白名单。
func ValidateWorkflow(wf *Workflow) error {
	if wf.Name == "" {
		return fmt.Errorf("workflow name is required")
	}
	if len(wf.Stages) == 0 {
		return fmt.Errorf("at least one stage is required")
	}
	for i, st := range wf.Stages {
		if st.Name == "" {
			return fmt.Errorf("stage[%d]: name is required", i)
		}
		if len(st.Jobs) == 0 {
			return fmt.Errorf("stage[%d]: at least one job is required", i)
		}
		for j, jb := range st.Jobs {
			if jb.Name == "" {
				return fmt.Errorf("stage[%d].job[%d]: name is required", i, j)
			}
			switch jb.JobType {
			case JobTypeFreestyle: // 兼容历史图纸：空 type 视作 freestyle
			case JobTypeDeploy:
			case "":
				jb.JobType = JobTypeFreestyle
			default:
				return fmt.Errorf("stage[%d].job[%d]: only freestyle|deploy is supported, got %q", i, j, jb.JobType)
			}
			// T2.2：deploy job 必须带 DeploySpec；freestyle 必须带 Spec（历史图纸走默认分支已归位）
			if jb.JobType == JobTypeDeploy {
				if jb.DeploySpec == nil {
					return fmt.Errorf("stage[%d].job[%d]: deploy job requires deploy_spec", i, j)
				}
				if jb.DeploySpec.Env == "" || jb.DeploySpec.DeploymentName == "" || jb.DeploySpec.ContainerName == "" || jb.DeploySpec.Image == "" {
					return fmt.Errorf("stage[%d].job[%d]: deploy_spec requires env/deployment_name/container_name/image", i, j)
				}
				continue
			}
			if jb.Spec == nil {
				return fmt.Errorf("stage[%d].job[%d]: spec is required", i, j)
			}
			// CI/CD 起 image 可空：引擎按步骤需要选默认（git/docker_build→mini-runner，纯 shell→busybox）。
			// steps/scripts 至少其一——引擎只认 steps，scripts 会翻译成隐式 step "main"（EffectiveSteps）
			if len(jb.Spec.Steps) == 0 && len(jb.Spec.Scripts) == 0 {
				return fmt.Errorf("stage[%d].job[%d]: spec.steps or spec.scripts is required", i, j)
			}
			// T1.9：错误策略白名单（拼错的 "ignor" 悄悄变 stop 比报错更危险）
			switch jb.Spec.ErrorPolicy {
			case "", ErrorPolicyStop, ErrorPolicyRetry, ErrorPolicyIgnore:
			default:
				return fmt.Errorf("stage[%d].job[%d]: unknown error_policy %q (want stop|retry|ignore)", i, j, jb.Spec.ErrorPolicy)
			}
			for _, ss := range jb.Spec.Steps {
				if ss.Name == "" {
					return fmt.Errorf("stage[%d].job[%d]: step name is required", i, j)
				}
				switch ss.Type {
				case StepShell, StepGit, StepDockerBuild, StepJunitReport, StepArchive:
				default:
					return fmt.Errorf("stage[%d].job[%d]: unknown step type %q", i, j, ss.Type)
				}
			}
		}
	}
	return nil
}
