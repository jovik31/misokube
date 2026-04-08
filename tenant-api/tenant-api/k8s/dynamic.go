package k8s

import (
	"os"
	"path/filepath"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func NewDynamicClient() (dynamic.Interface, error) {

	config, err := rest.InClusterConfig()
	if err != nil {
		home := os.Getenv("HOME")
		kubeconfig := filepath.Join(home, ".kube", "config")

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
	}

	return dynamic.NewForConfig(config)
}
