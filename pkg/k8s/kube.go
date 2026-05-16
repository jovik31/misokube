package k8s

import (
	"context"
	"flag"
	"fmt"
	"github/setera/pkg/generated/clientset/versioned"
	"log"
	"os"
	"path/filepath"
	"strings"

	// k8s client-go
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// [ ]: Implement kubeconfig intialization

func InitKubeConfig() (*rest.Config, error) {

	var kubeconfig *string

	if home := homedir.HomeDir(); home != "" {

		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}

	flag.Parse()
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Printf("Error in building kubeconfig from flags: %v, using in cluster config", err)
		config, err = rest.InClusterConfig()
		if err != nil {
			log.Printf("Error in building in cluster config: %v", err)
			return nil, err
		}
	}

	return config, nil
}

func NewKubeClient(c *rest.Config) (*kubernetes.Clientset, error) {
	// [ ]: Create a new client

	kubeClientset, err := kubernetes.NewForConfig(c)
	if err != nil {
		log.Printf("Error in building kubernetes clientset: %v", err)
		return nil, err
	}
	return kubeClientset, nil
}

func InitClients(config *rest.Config) (*kubernetes.Clientset, versioned.Interface, error) {

	kubeClientset, err := NewKubeClient(config)
	if err != nil {
		log.Printf("Error in creating kubernetes client: %v", err)
		return nil, nil, err
	}

	seteraClientset, err := NewSeteraClient(config)
	if err != nil {
		log.Printf("Error in creating setera client: %v", err)
		return nil, nil, err
	}

	return kubeClientset, seteraClientset, nil
}

func GetNodeName(clientset *kubernetes.Clientset) (string, error) {

	// check the NODE_NAME env var
	nodename := os.Getenv("NODE_NAME")

	if nodename == "" {
		// If NODE_NAME is not set, try to get it from the pod's spec
		podName := os.Getenv("POD_NAME")
		podNamespace := os.Getenv("POD_NAMESPACE")

		if podName == "" || podNamespace == "" {
			return "", fmt.Errorf("POD_NAME and POD_NAMESPACE environment variables must be set if NODE_NAME is not provided")
		}
		pod, err := clientset.CoreV1().Pods(podNamespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to get pod %s in namespace %s: %v", podName, podNamespace, err)
		}
		nodename = pod.Spec.NodeName
		if nodename == "" {
			return "", fmt.Errorf("node name is empty in pod spec for pod %s in namespace %s", podName, podNamespace)
		}
	}
	return nodename, nil
}

func GetNodeCIDR(clientset *kubernetes.Clientset, nodeName string) (string, error) {
	node, err := clientset.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get node %s: %v", nodeName, err)
	}
	if node.Spec.PodCIDR == "" {
		return "", fmt.Errorf("node %s has empty PodCIDR", nodeName)
	}
	return node.Spec.PodCIDR, nil
}

func GetNodes(clientset *kubernetes.Clientset) ([]*v1.NodeList, error) {

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %v", err)
	}
	var nodeList []*v1.NodeList
	nodeList = append(nodeList, nodes)
	return nodeList, nil
}

func GetNodeIP(clientset *kubernetes.Clientset, nodeName string) (string, error) {

	// get from env var
	if ip := os.Getenv("NODE_IP"); ip != "" {
		return ip, nil
	}

	// fetch from k8s API
	node, err := clientset.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get node %s: %v", nodeName, err)
	}
	for _, addr := range node.Status.Addresses {
		if addr.Type == v1.NodeInternalIP {
			return addr.Address, nil
		}
	}
	return "", fmt.Errorf("node %s has no InternalIP address", nodeName)
}

func StoreTenantLabel(clientset *kubernetes.Clientset, labelKey, nodeName, tenant string) error {

	fullKey := labelKey + "." + tenant
	patch := fmt.Sprintf(`{"metadata": {"labels": {"%s": "Enabled"}}}`, fullKey)

	_, err := clientset.CoreV1().Nodes().Patch(context.TODO(), nodeName, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch node %s with tenant annotation: %v", nodeName, err)
	}

	legacyBase := strings.Replace(labelKey, ".tenant", "/tenant", 1)
	if legacyBase != labelKey {
		legacyKey := legacyBase + "." + tenant
		cleanup := fmt.Sprintf(`{"metadata": {"labels": {"%s": null}}}`, legacyKey)
		if _, err := clientset.CoreV1().Nodes().Patch(context.TODO(), nodeName, types.MergePatchType, []byte(cleanup), metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("failed to remove legacy label %s from node %s: %v", legacyKey, nodeName, err)
		}
	}
	return nil
}
