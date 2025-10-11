package orchestrator

import (

	//std
	"context"
	"net/http"

	//internals
	tenant_operator "github/setera/internal/orchestrator/operator"
	seterav1clientset "github/setera/pkg/generated/clientset/versioned"
	scoreCache "github/setera/pkg/nodescore"

	//kubernetes
	"k8s.io/client-go/kubernetes"
)

/* [ ] Create the orchestrator structure
[ ] Operator Logic
[ ] Server to receive best scores from nodes - sent whenever a new tenant is added, is merged, deleted and split
[ ] Store best scores in cache

*/

type Orchestrator struct {
	Operator    *tenant_operator.TenantOperator
	ScoreCache  *scoreCache.NodeScoreCache
	ScoreServer *http.Server
}

func NewOrchestrator(
	ctx context.Context,
	name string,
	seteraClient seterav1clientset.Interface,
	kubeClientset kubernetes.Interface,
	scoreAddr string,
) *Orchestrator {

	cache := scoreCache.NewNodeScoreCache()

	tenantOp := tenant_operator.NewTenantOperator(ctx, name, seteraClient, kubeClientset, cache)

	return &Orchestrator{
		Operator:   tenantOp,
		ScoreCache: cache,
	}
}
