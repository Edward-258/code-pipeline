package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// NewClient —— 双模式取 K8s client（对照 Zadig：集群内 InClusterConfig，集群外 kubeconfig）
//
//	引擎部署在 k3s 里（chart: ServiceAccount pipeline-engine）→ rest.InClusterConfig()
//	本机 go run 调试 → ~/.kube/config，context 由 K8S_CONTEXT 指定（如 k3s-heyuan）
func NewClient(context string) (*kubernetes.Clientset, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return kubernetes.NewForConfig(cfg)
	}

	kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
	if _, err := os.Stat(kubeconfig); err != nil {
		return nil, fmt.Errorf("not in cluster and no kubeconfig at %s: %w", kubeconfig, err)
	}

	overrides := &clientcmd.ConfigOverrides{}
	if context != "" {
		overrides.CurrentContext = context
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig},
		overrides,
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig %s: %w", kubeconfig, err)
	}
	return kubernetes.NewForConfig(cfg)
}
