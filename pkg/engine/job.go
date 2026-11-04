package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"pipeline/pkg/config"
	"pipeline/pkg/models"
)

// K8s 资源名约束：dns-1123（小写字母数字与 '-'，≤63 字符）
var jobNameSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

// jobName —— 命名规则 mp-<wf>-<taskID>-<stage>-<job>（对照 Zadig 的 job-<taskID>-… 前缀保唯一）。
// ⚠️ workflow 名必须进名字（T1.6 活体验收抓的老雷）：只有 taskID 时，两个不同 workflow
// 的同号任务 + 同名 stage/job = 同一个 K8s Job 名，幂等 Delete 会删掉别人家的 Job、
// Clean 会掀掉别人正在轮询的执行物。wf 名在任务档案里唯一（图纸名即主键），进名字后全局唯一。
func jobName(wfName string, taskID int64, stage, job string) string {
	name := fmt.Sprintf("mp-%s-%d-%s-%s", wfName, taskID, stage, job)
	name = strings.ToLower(name)
	name = jobNameSanitizer.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if len(name) > 63 {
		name = name[:63] // 截断只发生在超长名时；前缀 mp-<wf>-<taskID> 唯一性在此尺度下够用
	}
	return name
}

// buildJob —— 把 JobTask 翻译成 K8s Job（对照 Zadig jobcontroller/kubernetes.go 的极简版）
//
//	驱动脚本直接拼进 sh -c（我们的 jobexecutor = 引擎代码生成的 bash，见 driver.go）
//	backoffLimit=0 + RestartPolicy=Never：Pod 退出码 = job 结果，不重试（决断 #3 语义根基）
//	镜像三档：显式指定 > git/docker_build 在场用 pipeline-runner > 纯 shell 用 busybox。
//	runner 在 ACR 是私有镜像（与引擎镜像同实例，决断 #12 同链路），Job Pod 必须自带 pull secret——
//	引擎 Deployment 挂的 imagePullSecrets 不会继承给运行时创建的 Job
func buildJob(namespace string, t *models.WorkflowTask, jt *models.JobTask, script string) *batchv1.Job {
	spec := jt.Spec
	image := spec.Image
	if image == "" {
		if jobNeedsRunner(spec) {
			image = config.Load().RunnerImage // env RUNNER_IMAGE / chart values runner.image
		} else {
			image = "busybox"
		}
	}

	volumes := []corev1.Volume{{Name: "workspace", VolumeSource: corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{}, // 步骤间的工作区（git 检出 → build 上下文 → 报告）
	}}}
	mounts := []corev1.VolumeMount{{Name: "workspace", MountPath: "/workspace"}}
	if jobWantsDockerSock(spec) {
		// 决断 #5：fat 路线——挂宿主 docker.sock 遥控节点 daemon。≈节点 root，仅限学习集群
		volumes = append(volumes, corev1.Volume{Name: "docker-sock", VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: "/var/run/docker.sock", Type: ptr.To(corev1.HostPathSocket)},
		}})
		mounts = append(mounts, corev1.VolumeMount{Name: "docker-sock", MountPath: "/var/run/docker.sock"})
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jt.K8sJobName,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr.To(int32(0)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					// ACR 私有镜像凭证（chart secret.yaml 生成的 dockerconfigjson，按 registry host 匹配：
					// secret 里只有 ACR 条目，busybox 等 docker.io 公共镜像拉取不受影响）
					ImagePullSecrets: []corev1.LocalObjectReference{{Name: "acr-credential"}},
					// push 端点走宿主 IP 的 NodePort（worker-2 回环 NodePort 不通，决断 #3）。
					// registry:2 存储剥离 host 前缀 → kubelet 侧拉取名叫 localhost:5000/…，两条名字链路天然对齐
					Containers: []corev1.Container{{
						Name:       "runner",
						Image:      image,
						WorkingDir: "/workspace",
						Command:    []string{"/bin/sh", "-c"},
						Args:       []string{script},
						Env: []corev1.EnvVar{
							{Name: "MP_NODE_IP", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "status.hostIP", // downward API
							}}},
							{Name: "MP_NODE_REGISTRY", Value: "$(MP_NODE_IP):30500"}, // K8s 依赖 env 展开
							{Name: "MP_ATTEMPT", Value: strconv.Itoa(jt.Retry + 1)},  // T1.4：第几次尝试（重试脚本可确定性分支）
							{Name: "MP_ENGINE_URL", Value: config.Load().EngineURL},  // T2.4：archive 上传目标
							{Name: "MP_TASK_WF", Value: t.WorkflowName},              // T2.4：artifact 上传 URL 组装
							{Name: "MP_TASK_ID", Value: strconv.FormatInt(t.TaskID, 10)},
						},
						VolumeMounts: mounts,
					}},
					Volumes: volumes,
				},
			},
		},
	}
}

// jobNeedsRunner —— git/docker_build 步骤需要 busybox 没有的工具（git、docker CLI）
func jobNeedsRunner(spec *models.FreestyleSpec) bool {
	for _, s := range spec.EffectiveSteps() {
		if s.Type == models.StepGit || s.Type == models.StepDockerBuild {
			return true
		}
	}
	return false
}

func jobWantsDockerSock(spec *models.FreestyleSpec) bool {
	for _, s := range spec.EffectiveSteps() {
		if s.Type == models.StepDockerBuild {
			return true
		}
	}
	return false
}
