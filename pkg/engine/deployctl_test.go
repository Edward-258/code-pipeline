package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"pipeline/pkg/models"
)

// T2.2 deploy job 单测（fake clientset）：不起 K8s Job 不挂 sock（不变量3），
// 引擎 client-go 直接 patch。fake 不模拟控制器行为，rollout 状态由测试预先摆好
// （"追上"剧本用 reactor 在每次 Get 时对齐 generation——模拟真实 rollout 控制器）。

func fastPoll(t *testing.T) {
	t.Helper()
	oldPoll, oldWait := pollInterval, rolloutWaitTimeout
	pollInterval = 10 * time.Millisecond
	rolloutWaitTimeout = 300 * time.Millisecond
	t.Cleanup(func() { pollInterval, rolloutWaitTimeout = oldPoll, oldWait })
}

func deployFixture(t *testing.T, mutate func(*appsv1.Deployment)) (*taskCtx, *models.JobTask, *fake.Clientset) {
	t.Helper()
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "nginx", Namespace: "mvp-dev"},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{
				{Name: "web", Image: "nginx:1.0"},
				{Name: "sidecar", Image: "busybox"},
			}}},
		},
	}
	if mutate != nil {
		mutate(dep)
	}
	fc := fake.NewSimpleClientset(dep)
	ds := &models.DeploySpec{Env: "dev", DeploymentName: "nginx", ContainerName: "web", Image: "nginx:2.0"}
	jt := &models.JobTask{Name: "ship", JobType: models.JobTypeDeploy, DeploySpec: ds}
	tk := &models.WorkflowTask{ID: "wf-1", WorkflowName: "wf", TaskID: 1}
	tc := &taskCtx{client: fc, ns: "pipeline", t: tk}

	old := envResolver
	envResolver = func(string) (string, error) { return "mvp-dev", nil }
	t.Cleanup(func() { envResolver = old })

	return tc, jt, fc
}

// catchUpReactor —— 每次 Get 都模拟"rollout 控制器已追上"：observedGeneration 对齐
// generation、新副本数齐、无不可用副本（fake 没有控制器，用 reactor 顶上）
func catchUpReactor(fc *fake.Clientset) {
	fc.PrependReactor("get", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		ga := action.(ktesting.GetAction)
		d, err := fc.Tracker().Get(ga.GetResource().GroupResource().WithVersion(ga.GetResource().Version), ga.GetNamespace(), ga.GetName())
		if err != nil {
			return false, nil, err
		}
		dep := d.(*appsv1.Deployment).DeepCopy()
		dep.Status.ObservedGeneration = dep.Generation
		desired := int32(1)
		if dep.Spec.Replicas != nil {
			desired = *dep.Spec.Replicas
		}
		dep.Status.UpdatedReplicas = desired
		dep.Status.UnavailableReplicas = 0
		return true, dep, nil
	})
}

// wait_rollout=false：patch 即成功返回，目标容器镜像已换、兄弟容器原样
func TestDeployPatchNoWait(t *testing.T) {
	tc, jt, fc := deployFixture(t, nil)
	jt.DeploySpec.WaitRollout = ptr.To(false) // 显式关掉等待（默认 true 会进 rollout 轮询）

	if err := (DeployJobCtl{}).Run(tc, jt); err != nil {
		t.Fatalf("Run: %v", err)
	}
	dep, _ := fc.AppsV1().Deployments("mvp-dev").Get(context.TODO(), "nginx", metav1.GetOptions{})
	if got := dep.Spec.Template.Spec.Containers[0].Image; got != "nginx:2.0" {
		t.Errorf("web image = %q, want nginx:2.0", got)
	}
	if got := dep.Spec.Template.Spec.Containers[1].Image; got != "busybox" {
		t.Errorf("sidecar image must stay: %q", got)
	}
}

// wait_rollout=true + 控制器追上：轮询通过
func TestDeployWaitRolloutDone(t *testing.T) {
	fastPoll(t)
	tc, jt, fc := deployFixture(t, func(d *appsv1.Deployment) { d.Generation = 1 })
	catchUpReactor(fc)
	if err := (DeployJobCtl{}).Run(tc, jt); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// wait_rollout=true + rollout 卡死：轮询超时报错
func TestDeployWaitRolloutTimeout(t *testing.T) {
	fastPoll(t)
	tc, jt, _ := deployFixture(t, func(d *appsv1.Deployment) {
		d.Generation = 1
		d.Status = appsv1.DeploymentStatus{ObservedGeneration: 0} // 控制器没见到新规格 → 永不完成
	})
	err := (DeployJobCtl{}).Run(tc, jt)
	if err == nil || !strings.Contains(err.Error(), "rollout timeout") {
		t.Fatalf("want rollout timeout, got %v", err)
	}
}

// env 解析失败：找不到 env 的 deploy job 在 patch 前就报错（不浪费一次 API 调用）
func TestDeployBadEnv(t *testing.T) {
	tc, jt, _ := deployFixture(t, nil)
	old := envResolver // 盖在 fixture 的 resolver 之上（fixture 的 cleanup 会恢复到它的 stub，无所谓）
	envResolver = func(string) (string, error) { return "", fmt.Errorf("no such env") }
	t.Cleanup(func() { envResolver = old })

	if err := (DeployJobCtl{}).Run(tc, jt); err == nil {
		t.Fatal("bad env should fail before patch")
	}
}

// 取消感知：轮询循环一拍内返回 errCancelled（patch 已发生不回滚，取消≠撤销部署）
func TestDeployCancelDuringWait(t *testing.T) {
	fastPoll(t)
	tc, jt, _ := deployFixture(t, func(d *appsv1.Deployment) {
		d.Generation = 1
		d.Status = appsv1.DeploymentStatus{ObservedGeneration: 0} // 永不完成，等取消
	})
	ch := make(chan struct{})
	close(ch)
	tc.cancelCh = ch
	if err := (DeployJobCtl{}).Run(tc, jt); err != errCancelled {
		t.Fatalf("want errCancelled, got %v", err)
	}
}
