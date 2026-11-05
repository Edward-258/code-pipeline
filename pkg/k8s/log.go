package k8s

import (
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"pipeline/pkg/config"
)

// Pod 日志 I/O（从 engine/logs.go 拆出的 I/O 半；banner 解析纯函数在 pkg/step）。

const tailCap = 64 * 1024 // 活日志尾部上限（与 step.maxStepLog 同值的 I/O 侧副本，避免层反向依赖）

// TailJobLog —— 活任务的日志尾部（api 日志端点透传用）。
// Job 已不存在（跑完被 Clean / 从未创建）→ ok=false，调用方回落到档案日志。
func TailJobLog(jobName string) (string, bool) {
	cfg := config.Load()
	client, err := NewClient(cfg.K8SContext)
	if err != nil {
		return "", false
	}
	if _, err := client.BatchV1().Jobs(cfg.K8SNamespace).Get(context.TODO(), jobName, metav1.GetOptions{}); err != nil {
		return "", false
	}
	txt, err := PodLog(client, cfg.K8SNamespace, jobName)
	if err != nil {
		return "", false
	}
	if len(txt) > tailCap {
		txt = "...[tail]...\n" + txt[len(txt)-tailCap:]
	}
	return txt, true
}

// PodLog —— 拉一个 Job 名下 runner Pod 的完整日志（创建窗口期 Pod 还没起 → error，调用方跳过）
func PodLog(client kubernetes.Interface, ns, jobName string) (string, error) {
	pods, err := client.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{LabelSelector: "job-name=" + jobName})
	if err != nil {
		return "", err
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pod for job %s yet", jobName)
	}
	req := client.CoreV1().Pods(ns).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{Container: "runner"})
	stream, err := req.Stream(context.TODO())
	if err != nil {
		return "", err
	}
	defer stream.Close()
	b, err := io.ReadAll(stream)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
