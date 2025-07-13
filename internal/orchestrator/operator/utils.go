package orchestrator

import (
	"context"

	seterav1 "github/setera/pkg/api/setera.com/v1"

	// k8s
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetTenantsByNode returns all tenants whose spec.nodes includes the given nodeName.
/*func GetTenantsByNode(nodeName string, tenantLister v1.TenantLister) ([]*seterav1.Tenant, error) {
	allTenants, err := tenantLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("failed to list tenants: %w", err)
	}

	var result []*seterav1.Tenant
	for _, tenant := range allTenants {
		if containsNode(tenant.Spec.Nodes, nodeName) {
			result = append(result, tenant)
		}
	}
	return result, nil
}

// containsNode returns true if the nodeName is in the list.
func containsNode(nodes []seterav1.Node, nodeName string) bool {
	for _, n := range nodes {
		if n.Name == nodeName {
			return true
		}
	}
	return false
}
*/

// checks if tenant has the finalizer
func (t *TenantOperator) checkTenantFinalizer(tenant *seterav1.Tenant) bool {

	for _, f := range tenant.Finalizers {
		if f == TenantFinalizer {
			return true // finalizer found
		}
	}
	return false // finalizer not found

}

func (t *TenantOperator) updateTenantObject(tenant *seterav1.Tenant) error {

	// update the tenant object in the k8s cluster
	_, err := t.Base.Seterav1Clientset.SeteraV1().Tenants(tenant.Namespace).Update(context.TODO(), tenant, metav1.UpdateOptions{})
	if err != nil {
		t.Base.Logger.Error(err, "Error in updating tenant object in the k8s cluster", tenant.Name)
		return err
	}
	return nil
}

func ContainsString(slice []string, item string) bool {

	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
