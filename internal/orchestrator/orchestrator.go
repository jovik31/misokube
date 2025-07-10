package orchestrator

import (

	//internals
	tenant_operator "github/setera/internal/orchestrator/operator"

	//std
	"net/http"
)

/* [ ] Create the orchestrator structure
[ ] Operator Logic
[ ] Server to receive best scores from nodes - sent whenever a new tenant is added, is merged, deleted and split
[ ] Store best scores in cache

*/

type orchestrator struct {
	Operator    *tenant_operator.TenantOperator
	ScoreCache  *NodeScoreCache
	ScoreServer *http.Server
}
