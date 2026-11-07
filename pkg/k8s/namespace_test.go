package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// EnsureNamespace 四例（fake clientset，无需真集群）：随建带标签 / Active 放过 /
// Terminating 拒绝 / 并发 AlreadyExists 容错
func TestEnsureNamespace(t *testing.T) {
	t.Run("不存在→创建且带managed-by标签", func(t *testing.T) {
		cli := k8sfake.NewSimpleClientset()
		if err := EnsureNamespace(cli, "mp-fresh"); err != nil {
			t.Fatalf("ensure: %v", err)
		}
		got, err := cli.CoreV1().Namespaces().Get(context.TODO(), "mp-fresh", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get after ensure: %v", err)
		}
		if got.Labels["app.kubernetes.io/managed-by"] != "mini-pipeline" {
			t.Fatalf("managed-by label missing, labels=%v", got.Labels)
		}
	})

	t.Run("已存在Active→零操作", func(t *testing.T) {
		cli := k8sfake.NewSimpleClientset(&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "mp-here"},
		})
		if err := EnsureNamespace(cli, "mp-here"); err != nil {
			t.Fatalf("ensure on existing: %v", err)
		}
		for _, a := range cli.Actions() {
			if a.GetVerb() == "create" {
				t.Fatalf("existing Active ns must not be recreated, got action %v", a)
			}
		}
	})

	t.Run("Terminating→报错", func(t *testing.T) {
		cli := k8sfake.NewSimpleClientset(&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "mp-dying"},
			Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating},
		})
		if err := EnsureNamespace(cli, "mp-dying"); err == nil {
			t.Fatal("Terminating ns must be refused, got nil")
		}
	})

	t.Run("Create撞AlreadyExists→容错放过", func(t *testing.T) {
		cli := k8sfake.NewSimpleClientset()
		cli.PrependReactor("create", "namespaces", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewAlreadyExists(corev1.Resource("namespaces"), "mp-raced")
		})
		if err := EnsureNamespace(cli, "mp-raced"); err != nil {
			t.Fatalf("concurrent create race must be tolerated: %v", err)
		}
	})
}
