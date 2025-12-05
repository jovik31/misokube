package cniplugin

import (
	"encoding/json"
	"errors"
	"net"

	"github.com/containernetworking/cni/pkg/skel"
)

func checkReadiness(opt Options) error {
	d := net.Dialer{Timeout: opt.Timeout}
	c, err := d.Dial("unix", opt.SocketPath)
	if err != nil {
		return err
	}
	_ = c.Close()
	return nil
}

func validateArgs(a *skel.CmdArgs) error {
	if a.ContainerID == "" || a.Netns == "" || a.IfName == "" {
		return errors.New("missing CNI env (containerID/netns/ifName)")
	}
	if len(a.Netns) > 512 {
		return errors.New("netns path too long")
	}
	return nil
}

func detectCNIVersion(stdin []byte) string {
	type c struct {
		CNIVersion string `json:"cniVersion"`
	}
	var v c
	if err := json.Unmarshal(stdin, &v); err == nil && v.CNIVersion != "" {
		return v.CNIVersion
	}
	return "1.1.0"
}

func nonEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
