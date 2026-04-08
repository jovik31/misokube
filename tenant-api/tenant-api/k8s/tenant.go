package k8s

import (
	"context"

	"k8s.io/client-go/dynamic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"strings"
	"log"
)

func CreateTenantCRD(
	dynClient dynamic.Interface,
	name string,
	zones int,
) error {

	gvr := schema.GroupVersionResource{
		Group:    "setera.com",    // Correct
		Version:  "v1",
		Resource: "tenants",
	}

	tenant := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "setera.com/v1",
			"kind":       "Tenant",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"name":		 name,
				"zones":     zones,
			},
		},
	}

	_, err := dynClient.
		Resource(gvr).
		Create(context.TODO(), tenant, metav1.CreateOptions{})

	if err != nil {
		// Check if it's just telling us it already exists (meaning it worked)
		if strings.Contains(err.Error(), "already exists") {
			log.Println("Resource actually exists, ignoring error.")
			return nil
		}
		return err
	}

	log.Printf("Successfully created:", tenant)

	return err
}
