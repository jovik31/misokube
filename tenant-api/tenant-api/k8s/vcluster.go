package k8s

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
)

const (
	nodePortFirst = int32(30000)
	nodePortLast  = int32(32767)
)

var nodePortAllocationMu sync.Mutex

// VClusterExternalHost is the address written into returned kubeconfigs.
func VClusterExternalHost() string {
	if host := strings.TrimSpace(os.Getenv("VCLUSTER_EXTERNAL_HOST")); host != "" {
		return host
	}
	return "192.168.1.84"
}

// AllocateVClusterNodePort chooses a stable free NodePort. Existing vClusters
// keep their current port so repeated requests are idempotent.
func AllocateVClusterNodePort(
	ctx context.Context,
	client *kubernetes.Clientset,
	name,
	namespace string,
) (int32, error) {
	nodePortAllocationMu.Lock()
	defer nodePortAllocationMu.Unlock()

	service, err := client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		for _, port := range service.Spec.Ports {
			if port.NodePort != 0 && (port.Port == 443 || port.Name == "https") {
				return port.NodePort, nil
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return 0, fmt.Errorf("get existing vCluster service %s/%s: %w", namespace, name, err)
	}

	services, err := client.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("list Services for NodePort allocation: %w", err)
	}
	used := make(map[int32]struct{})
	for _, item := range services.Items {
		for _, port := range item.Spec.Ports {
			if port.NodePort != 0 {
				used[port.NodePort] = struct{}{}
			}
		}
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	rangeSize := int32(nodePortLast - nodePortFirst + 1)
	start := nodePortFirst + int32(h.Sum32()%uint32(rangeSize))
	for offset := int32(0); offset < rangeSize; offset++ {
		candidate := nodePortFirst + ((start - nodePortFirst + offset) % rangeSize)
		if _, exists := used[candidate]; !exists {
			return candidate, nil
		}
	}
	return 0, fmt.Errorf("no free NodePort in range %d-%d", nodePortFirst, nodePortLast)
}

func CreateVClusterCLI(
	ctx context.Context,
	name,
	namespace,
	externalHost string,
	nodePort int32,
) error {
	values, err := os.CreateTemp("", "vcluster-values-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary vCluster values: %w", err)
	}
	valuesPath := values.Name()
	defer os.Remove(valuesPath)

	config := fmt.Sprintf(`controlPlane:
  proxy:
    extraSANs:
      - %q
  service:
    httpsNodePort: %d
    spec:
      type: NodePort
  statefulSet:
    pods:
      labels:
        misokube.com/tenant: "default"
    scheduling:
      nodeSelector:
        %q: "true"
    persistence:
      volumeClaim:
        enabled: false
`, externalHost, nodePort, "misokube.com/tenant."+name)
	if _, err := values.WriteString(config); err != nil {
		_ = values.Close()
		return fmt.Errorf("write temporary vCluster values: %w", err)
	}
	if err := values.Close(); err != nil {
		return fmt.Errorf("close temporary vCluster values: %w", err)
	}

	commandCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(
		commandCtx,
		"vcluster", "create", name,
		"-n", namespace,
		"--upgrade",
		"--connect=false",
		"--values", valuesPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		if commandCtx.Err() != nil {
			return fmt.Errorf("vcluster create timed out: %w", commandCtx.Err())
		}
		return fmt.Errorf("vcluster create failed: %v, output: %s", err, string(output))
	}

	log.Println("vCluster created:", string(output))
	return nil
}

// ConfigureVClusterHostNetwork bypasses the Kubernetes Service path for the
// trusted vCluster control plane. MIsoKube's current eBPF data plane does not
// implement ClusterIP forwarding, so the syncer must contact the host API
// directly through the node network.
func ConfigureVClusterHostNetwork(
	ctx context.Context,
	client *kubernetes.Clientset,
	name,
	namespace,
	hostAPIAddress string,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get vCluster Deployment %s/%s: %w", namespace, name, err)
		}

		deployment.Spec.Strategy.Type = appsv1.RecreateDeploymentStrategyType
		deployment.Spec.Strategy.RollingUpdate = nil
		deployment.Spec.Template.Spec.HostNetwork = true
		deployment.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirstWithHostNet

		for index := range deployment.Spec.Template.Spec.Containers {
			container := &deployment.Spec.Template.Spec.Containers[index]
			if container.Name != "syncer" {
				continue
			}
			setContainerEnv(container, "KUBERNETES_SERVICE_HOST", hostAPIAddress)
			setContainerEnv(container, "KUBERNETES_SERVICE_PORT", "6443")
			setContainerEnv(container, "KUBERNETES_SERVICE_PORT_HTTPS", "6443")
		}

		if _, err := client.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update vCluster Deployment %s/%s for host networking: %w", namespace, name, err)
		}
		return nil
	})
}

func setContainerEnv(container *corev1.Container, name, value string) {
	for index := range container.Env {
		if container.Env[index].Name == name {
			container.Env[index].Value = value
			container.Env[index].ValueFrom = nil
			return
		}
	}
	container.Env = append(container.Env, corev1.EnvVar{Name: name, Value: value})
}

func WaitVClusterReady(
	ctx context.Context,
	client *kubernetes.Clientset,
	name,
	namespace string,
	timeout time.Duration,
) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastState string
	for {
		deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			desired := int32(0)
			if deployment.Spec.Replicas != nil {
				desired = *deployment.Spec.Replicas
			}
			lastState = fmt.Sprintf("ready=%d available=%d desired=%d", deployment.Status.ReadyReplicas, deployment.Status.AvailableReplicas, desired)
			if deployment.Status.AvailableReplicas >= 1 {
				return nil
			}
		} else if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get vCluster Deployment %s/%s: %w", namespace, name, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for vCluster %s/%s to become ready (%s)", namespace, name, lastState)
		case <-ticker.C:
		}
	}
}

// GetVClusterKubeconfig retrieves the generated kubeconfig and writes the
// externally reachable endpoint into it while preserving its CA and credentials.
func GetVClusterKubeconfig(
	ctx context.Context,
	client *kubernetes.Clientset,
	name,
	namespace,
	endpoint string,
) (string, error) {
	secretNames := []string{"vc-config-" + name, "vc-" + name}

	var lastErr error
	for i := 0; i < 60; i++ {
		for _, secretName := range secretNames {
			secret, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
			if err != nil {
				lastErr = err
				continue
			}

			data, ok := secret.Data["config"]
			if !ok || len(data) == 0 {
				keys := make([]string, 0, len(secret.Data))
				for key := range secret.Data {
					keys = append(keys, key)
				}
				lastErr = fmt.Errorf("secret %s/%s exists but no 'config' key (keys: %v)", namespace, secretName, keys)
				continue
			}

			config, err := clientcmd.Load(data)
			if err != nil {
				return "", fmt.Errorf("parse kubeconfig from %s/%s: %w", namespace, secretName, err)
			}
			for _, cluster := range config.Clusters {
				cluster.Server = endpoint
			}
			rewritten, err := clientcmd.Write(*config)
			if err != nil {
				return "", fmt.Errorf("serialize external vCluster kubeconfig: %w", err)
			}

			log.Printf("found kubeconfig in secret %s/%s (%d bytes)", namespace, secretName, len(rewritten))
			return string(rewritten), nil
		}

		log.Printf("waiting for kubeconfig secret %s/%v (attempt %d/60): %v", namespace, secretNames, i+1, lastErr)
		if err := waitForRetry(ctx); err != nil {
			return "", err
		}
	}

	return "", fmt.Errorf("timed out waiting for kubeconfig secret %s/%v: %w", namespace, secretNames, lastErr)
}

func waitForRetry(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return nil
	}
}
