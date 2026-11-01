package engine

import (
	"fmt"
	"regexp"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"pipeline/models"
)

// K8s 资源名约束：dns-1123（小写字母数字与 '-'，≤63 字符）
var jobNameSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

// jobName —— 命名规则 mp-<taskID>-<stage>-<job>（对照 Zadig 的 job-<taskID>-… 前缀保唯一）
func jobName(taskID int64, stage, job string) string {
	name := fmt.Sprintf("mp-%d-%s-%s", taskID, stage, job)
	name = strings.ToLower(name)
	name = jobNameSanitizer.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if len(name) > 63 {
		name = name[:63] // 截断只会发生在超长 stage/job 名时，前缀 mp-<taskID> 保证唯一性
	}
	return name
}

// buildJob —— 把 JobTask 翻译成 K8s Job（对照 Zadig jobcontroller/kubernetes.go 的极简版）
//
//	脚本直接拼进 sh -c（M1 不用 ConfigMap，学习版最短路径）
//	backoffLimit=0 + RestartPolicy=Never：Pod 退出码 = job 结果，不重试（这是决断 #3 的语义根基）
func buildJob(namespace string, jt *models.JobTask) *batchv1.Job {
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
					Containers: []corev1.Container{
						{
							Name:    "runner",
							Image:   jt.Spec.Image,
							Command: []string{"/bin/sh", "-c"},
							Args:    []string{strings.Join(jt.Spec.Scripts, "\n")},
						},
					},
				},
			},
		},
	}
}
