package server

import (
	"net/http"

	"tenant-api/k8s"

	"github.com/gin-gonic/gin"

	"log"
)

type CreateTenantRequest struct {
	Name string `json:"name"`
	Zones int `json:"zones"`
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

	log.Println("Tenant request:", req)

	client, err := k8s.NewClient()
	if err != nil {
		log.Println("Client Error", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	namespace := "setera-" + req.Name

	if err := k8s.NewNamespace(client, namespace); err != nil {
		log.Println("Namespace Error", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	dynClient, err := k8s.NewDynamicClient()
	if err != nil {
		log.Println("Dynamic Client Error:", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	err = k8s.CreateTenantCRD(dynClient, req.Name, req.Zones)
	if err != nil {
		log.Println("Create CRD Error:", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	err = k8s.CreateVClusterCLI(req.Name, namespace)
	if err != nil {
		log.Println("vCluster creation error:", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	kubeconfig, err := k8s.GetVClusterKubeconfig(client, req.Name, namespace)
	if err != nil {
		log.Println("vCluster kubeconfig error:", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tenant":     req.Name,
		"kubeconfig": kubeconfig,
	})


	// c.JSON(http.StatusOK, gin.H{
	// 	"tenant": req.Name,
	// 	"zones": req.Zones,
	// 	"status": "creating",
	// })
}
