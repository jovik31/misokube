package netiptable

import (
	"github.com/coreos/go-iptables/iptables"
)

type NetlinkIPTableHandle interface {
	NewChain(table, chain string) error
	ClearChain(table, chain string) error
	DeleteChain(table, chain string) error
	AppendUnique(table, chain string, rulespec ...string) error
	InsertUnique(table, chain string, pos int, rulespec ...string) error
	Delete(table, chain string, rulespec ...string) error
}

type rNetlinkIPTableHandle struct {
	ipt *iptables.IPTables
}

func (r rNetlinkIPTableHandle) NewChain(table, chain string) error {
	return r.ipt.NewChain(table, chain)
}
func (r rNetlinkIPTableHandle) ClearChain(table, chain string) error {
	return r.ipt.ClearChain(table, chain)
}
func (r rNetlinkIPTableHandle) DeleteChain(table, chain string) error {
	return r.ipt.DeleteChain(table, chain)
}
func (r rNetlinkIPTableHandle) AppendUnique(table, chain string, rulespec ...string) error {
	return r.ipt.AppendUnique(table, chain, rulespec...)
}
func (r rNetlinkIPTableHandle) InsertUnique(table, chain string, pos int, rulespec ...string) error {
	return r.ipt.InsertUnique(table, chain, pos, rulespec...)
}
func (r rNetlinkIPTableHandle) Delete(table, chain string, rulespec ...string) error {
	return r.ipt.Delete(table, chain, rulespec...)
}
