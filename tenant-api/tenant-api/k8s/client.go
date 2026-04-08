package k8s

import (
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/rest"
)

func NewClient() (*kubernetes.Clientset, error) {

	var config *rest.Config
	var err error

	// In cluster
	config, err = rest.InClusterConfig()
	if err != nil {
		// Local dev
		home := os.Getenv("HOME")
		kubeconfig := filepath.Join(home, ".kube", "config")

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
	}

	return kubernetes.NewForConfig(config)
}
