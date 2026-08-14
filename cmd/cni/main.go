package main

/* cni function
- Handle CNI ADD/CHECK/DEL requests
	- Parse CNI request
	- Create CNI Request object and send to NetworkManager via Unix socket
	- Wait for response and forward it to the CNI caller - the kubelet

*/

import (
	"os"
	"time"

	"github/setera/internal/cniplugin"

	"github.com/containernetworking/cni/pkg/skel"
	cniversion "github.com/containernetworking/cni/pkg/version"
)

func main() {
	// Initialize logging to file if configured; fallback is stderr.
	lf := cniplugin.LoadLogFile()
	if lf != nil {
		defer lf.Close()
	}

	opts := cniplugin.Options{
		SocketPath: "/var/run/setera/setera.sock",
		Timeout:    15 * time.Second,
	}

	skel.PluginMainFuncs(
		skel.CNIFuncs{
			Add: func(args *skel.CmdArgs) error {
				return cniplugin.Add(args, os.Stdout, opts)
			},
			Check: func(args *skel.CmdArgs) error {
				return cniplugin.Check(args, os.Stdout, opts)
			},
			Del: func(args *skel.CmdArgs) error {
				return cniplugin.Del(args, opts)
			},
			GC: func(args *skel.CmdArgs) error {
				return cniplugin.GC(args, os.Stdout, opts)
			},
			Status: func(args *skel.CmdArgs) error {
				return cniplugin.Status(args, os.Stdout, opts)
			},
		},

		cniversion.All, "setera cni plugin, delegates cni requests to setera daemon via unix socket",
	)

}
