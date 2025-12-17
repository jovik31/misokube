package tenant

import (
	"github.com/coreos/go-iptables/iptables"
)

type NetlinkTenantPolicyHandle interface {
	NewChain(table, chain string) error
	ClearChain(table, chain string) error
	DeleteChain(table, chain string) error
	AppendUnique(table, chain string, rulespec ...string) error
	InsertUnique(table, chain string, pos int, rulespec ...string) error
	Delete(table, chain string, rulespec ...string) error
	List(table, chain string) ([]string, error)
}

type rNetlinkTenantPolicyHandle struct {
	ipt *iptables.IPTables
}

func (r rNetlinkTenantPolicyHandle) NewChain(table, chain string) error {
	return r.ipt.NewChain(table, chain)
}
func (r rNetlinkTenantPolicyHandle) ClearChain(table, chain string) error {
	return r.ipt.ClearChain(table, chain)
}
func (r rNetlinkTenantPolicyHandle) DeleteChain(table, chain string) error {
	return r.ipt.DeleteChain(table, chain)
}
func (r rNetlinkTenantPolicyHandle) AppendUnique(table, chain string, rulespec ...string) error {
	return r.ipt.AppendUnique(table, chain, rulespec...)
}
func (r rNetlinkTenantPolicyHandle) InsertUnique(table, chain string, pos int, rulespec ...string) error {
	return r.ipt.InsertUnique(table, chain, pos, rulespec...)
}
func (r rNetlinkTenantPolicyHandle) Delete(table, chain string, rulespec ...string) error {
	return r.ipt.Delete(table, chain, rulespec...)
}

func (r rNetlinkTenantPolicyHandle) List(table, chain string) ([]string, error) {
	return r.ipt.List(table, chain)
}
