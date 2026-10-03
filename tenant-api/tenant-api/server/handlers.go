package server

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"tenant-api/k8s"

	"github.com/gin-gonic/gin"
	"k8s.io/apimachinery/pkg/util/validation"
)

type CreateTenantRequest struct {
	Name  string `json:"name"`
	Zones int    `json:"zones"`
}

func HealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

func CreateTenantHandler(c *gin.Context) {
	var req CreateTenantRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if errors := validation.IsDNS1123Label(req.Name); len(errors) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("name must be a valid Kubernetes DNS label: %s", strings.Join(errors, "; ")),
		})
		return
	}
	if req.Zones < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "zones must be at least 1"})
		return
	}
	if req.Name == "default" || strings.HasPrefix(req.Name, "kube-") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "this tenant name is reserved"})
		return
	}

	log.Println("Tenant request:", req)

	client, err := k8s.NewClient()
	if err != nil {
		log.Println("Client Error", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	namespace := "misokube-" + req.Name
	if errors := validation.IsDNS1123Label(namespace); len(errors) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("generated namespace is invalid: %s", strings.Join(errors, "; ")),
		})
		return
	}

	dynClient, err := k8s.NewDynamicClient()
	if err != nil {
		log.Println("Dynamic Client Error:", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	err = k8s.EnsureTenant(c.Request.Context(), dynClient, req.Name, req.Zones)
	if err != nil {
		log.Println("Create CRD Error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := k8s.WaitTenantAssigned(c.Request.Context(), dynClient, req.Name, 3*time.Minute); err != nil {
		log.Println("Tenant assignment error:", err)
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	if err := k8s.EnsureNamespace(c.Request.Context(), client, namespace, req.Name); err != nil {
		log.Println("Namespace Error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	externalHost := k8s.VClusterExternalHost()
	nodePort, err := k8s.AllocateVClusterNodePort(c.Request.Context(), client, req.Name, namespace)
	if err != nil {
		log.Println("NodePort allocation error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	err = k8s.CreateVClusterCLI(c.Request.Context(), req.Name, namespace, externalHost, nodePort)
	if err != nil {
		log.Println("vCluster creation error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := k8s.ConfigureVClusterHostNetwork(c.Request.Context(), client, req.Name, namespace, externalHost); err != nil {
		log.Println("vCluster host-network configuration error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := k8s.WaitVClusterReady(c.Request.Context(), client, req.Name, namespace, 5*time.Minute); err != nil {
		log.Println("vCluster readiness error:", err)
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": err.Error()})
		return
	}

	endpoint := fmt.Sprintf("https://%s:%d", externalHost, nodePort)
	kubeconfig, err := k8s.GetVClusterKubeconfig(c.Request.Context(), client, req.Name, namespace, endpoint)
	if err != nil {
		log.Println("vCluster kubeconfig error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tenant":     req.Name,
		"namespace":  namespace,
		"endpoint":   endpoint,
		"kubeconfig": kubeconfig,
	})
}
