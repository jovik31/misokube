package k8s

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func CreateVClusterCLI(name, namespace string) error {
	cmd := exec.Command(
		"vcluster", "create", name,
		"-n", namespace,
		"--connect=false",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("vcluster create failed: %v, output: %s", err, string(output))
	}

	log.Println("vCluster created:", string(output))
	return nil
}

// GetVClusterKubeconfig retrieves the kubeconfig for a vcluster
// by reading the "config" key from the vc-<name> secret in the namespace.
// It accepts an existing client to avoid creating a new one that might
// point to the wrong cluster context.
func GetVClusterKubeconfig(client *kubernetes.Clientset, name, namespace string) (string, error) {
	secretName := "vc-" + name

	// The secret takes some time to appear after vcluster create --connect=false.
	// Poll every 5s for up to 2 minutes.
	var lastErr error
	for i := 0; i < 24; i++ {
		secret, err := client.CoreV1().Secrets(namespace).Get(
			context.TODO(), secretName, metav1.GetOptions{},
		)
		if err != nil {
			lastErr = err
			log.Printf("Waiting for secret %s/%s (attempt %d/24): %v", namespace, secretName, i+1, err)
			time.Sleep(5 * time.Second)
			continue
		}

		data, ok := secret.Data["config"]
		if !ok || len(data) == 0 {
			keys := make([]string, 0, len(secret.Data))
			for k := range secret.Data {
				keys = append(keys, k)
			}
			lastErr = fmt.Errorf("secret %s/%s exists but no 'config' key (keys: %v)", namespace, secretName, keys)
			log.Printf("Attempt %d/24: %v", i+1, lastErr)
			time.Sleep(5 * time.Second)
			continue
		}

		log.Printf("Found kubeconfig in secret %s/%s (%d bytes)", namespace, secretName, len(data))
		return string(data), nil
	}

	return "", fmt.Errorf("timed out waiting for kubeconfig secret %s/%s: %w", namespace, secretName, lastErr)
}
