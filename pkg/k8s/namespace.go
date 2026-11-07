package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// EnsureNamespace —— 幂等确保 namespace 存在（env 创建随建，Zadig 同款语义的极简版）。
// namespace 是集群级资源，Role（namespaced）够不着——活体 RBAC 靠 chart 配套的
// pipeline-engine-ns ClusterRole，verbs 刻意只有 get/create：建不删语义下永不需要
// delete（删 ns = 灭顶键，共享 ns 误删即连窝端，见 AGENTS §4 决断 #13）。
//
//   - 已存在 Active → 放过（重复 POST 同 env 幂等重建同 ns，无害）
//   - Terminating → 报错（防撞正在删的 ns，Zadig 同款检查）
//   - NotFound → Create，盖 managed-by 标签（溯源凭据：这个 ns 是引擎开的）
//   - Create 撞 AlreadyExists → 容错放过（并发双建，赢家的 ns 同样可用）
func EnsureNamespace(client kubernetes.Interface, name string) error {
	ns, err := client.CoreV1().Namespaces().Get(context.TODO(), name, metav1.GetOptions{})
	if err == nil {
		if ns.Status.Phase == corev1.NamespaceTerminating {
			return fmt.Errorf("namespace %s is Terminating, refuse to reuse", name)
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get namespace %s: %w", name, err)
	}
	_, err = client.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"app.kubernetes.io/managed-by": "mini-pipeline"},
		},
	}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s: %w", name, err)
	}
	return nil
}
