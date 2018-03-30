package kubernetes

import (
	"os"
	"path/filepath"

	"github.com/docker/docker/pkg/homedir"
	"k8s.io/client-go/tools/clientcmd"
)

// NewKubernetesConfig resolves the path to the desired Kubernetes configuration file, depending
// environment variable and command line flag.
func NewKubernetesConfig(configFlag string) clientcmd.ClientConfig {
	kubeConfig := configFlag
	if kubeConfig == "" {
		if config := os.Getenv("KUBECONFIG"); config != "" {
			kubeConfig = config
		} else {
			kubeConfig = filepath.Join(homedir.Get(), ".kube/config")
		}
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfig},
		&clientcmd.ConfigOverrides{})
}
