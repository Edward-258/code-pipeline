package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"pipeline/pkg/engine/cancel"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// DeployJobCtl —— deploy 作业（T2.2，CD 本体；对照 Zadig job_deploy.go 的极简版）。
// 不变量 3（mvp复现计划 §5）：deploy 是引擎 API 操作不是 Pod 内脚本——不起 K8s Job、
// 不挂 docker.sock，引擎直接 client-go patch Deployment 的镜像字段，然后轮询 rollout。
//
// 三件套与 K8sJobCtl 对齐：
//   Run   = patch 镜像 → 轮询 rollout（wait_rollout=false 时 patch 即成功返回——
//           为"自举发布自己更新自己"特化：防滚动更新绞杀正在跑的自己）
//   SaveInfo = 无事可做（没有 Pod 日志可收割）
//   Clean = 无事可做（Deployment 是存量资产不是消耗品——资源与数据分离的另一面）
//
// 取消语义：轮询期感知 cancel 即返回，但 patch 已发生不回滚（取消 ≠ 撤销部署）。
// JobTask.K8sJobName 对 deploy 恒空——日志端点自然回落到空档案。

// envResolver —— env 名 → namespace 的解析钩子（var 而非直呼 repo：单测可以 stub）
var envResolver = func(name string) (string, error) {
	e, err := repo.FindEnv(name)
	if err != nil {
		return "", fmt.Errorf("resolve env %q: %w", name, err)
	}
	return e.Namespace, nil
}

// rolloutWaitTimeout —— rollout 轮询上限（var：单测缩短，别让表测跑 10 分钟）
var rolloutWaitTimeout = jobTimeout

// DeployJobCtl —— deploy job 执行器（patch Deployment，不起 Pod）
type DeployJobCtl struct{}

func (DeployJobCtl) Run(c *Ctx, jt *models.JobTask) error {
	ds := jt.DeploySpec
	ns, err := envResolver(ds.Env)
	if err != nil {
		return err
	}

	// Strategic Merge Patch：containers 按 name 合并——只动目标容器的 image，
	// 其余容器/字段原样保留（对照 kubectl set image 的底层语义）
	patch, err := json.Marshal(map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []map[string]any{
						{"name": ds.ContainerName, "image": ds.Image},
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("build patch: %w", err)
	}
	if _, err := c.Client.AppsV1().Deployments(ns).Patch(context.TODO(), ds.DeploymentName, types.StrategicMergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch deployment %s/%s: %w", ns, ds.DeploymentName, err)
	}
	log.Printf("exec: task %s/%d deploy job %q -> deployment %s/%s image=%s", c.Task.WorkflowName, c.Task.TaskID, jt.Name, ns, ds.DeploymentName, ds.Image)
	if !ds.ShouldWaitRollout() {
		return nil
	}

	// rollout 轮询（kubectl rollout status 的简化版）：
	//   observedGeneration 追上 patch 后的 metadata.generation（控制器已见到新规格）
	//   && updatedReplicas == desired（新版本全量更新）
	//   && unavailableReplicas == 0（没有旧 Pod 苟着）
	deadline := time.Now().Add(rolloutWaitTimeout)
	for {
		if cancel.Fired(c.CancelCh) {
			return cancel.ErrCancelled
		}
		d, err := c.Client.AppsV1().Deployments(ns).Get(context.TODO(), ds.DeploymentName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("poll deployment %s/%s: %w", ns, ds.DeploymentName, err)
		}
		if rolloutDone(d) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("rollout timeout after %s (deployment %s/%s)", rolloutWaitTimeout, ns, ds.DeploymentName)
		}
		time.Sleep(pollInterval)
	}
}

// rolloutDone —— generation 对齐 + 新副本数齐 + 无不可用副本
func rolloutDone(d *appsv1.Deployment) bool {
	if d.Generation == 0 || d.Status.ObservedGeneration < d.Generation {
		return false
	}
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	return d.Status.UpdatedReplicas >= desired && d.Status.UnavailableReplicas == 0
}

func (DeployJobCtl) SaveInfo(c *Ctx, jt *models.JobTask) error { return nil }

func (DeployJobCtl) Clean(c *Ctx, jt *models.JobTask) {}
