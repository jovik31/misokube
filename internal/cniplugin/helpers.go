package cniplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"regexp"

	"github/setera/pkg/wire"

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

// Check errors with regex
func get_pod_name_regex(arg string) string {

	var re = regexp.MustCompile(`(-?)K8S_POD_NAME=(.+?)(;|$)`)
	mf := re.FindStringSubmatch(arg)
	return mf[2]

}

func get_pod_namespace_regex(arg string) string {

	var re = regexp.MustCompile(`(-?)K8S_POD_NAMESPACE=(.+?)(;|$)`)
	mf := re.FindStringSubmatch(arg)
	return mf[2]
}

func get_pod_uid_regex(arg string) string {

	var re = regexp.MustCompile(`(-?)K8S_POD_UID=(.+?)(;|$)`)
	mf := re.FindStringSubmatch(arg)
	return mf[2]
}

// computeIdemKey returns a stable idempotency key for a CNI operation.
// It hashes a subset of fields that uniquely identify the intent.
func computeIdemKey(cmd wire.Command, containerID, netns, ifname, podNS, podName, podUID string) string {
	h := sha256.New()
	// Use NUL separators to avoid collisions.
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write(string(cmd))
	write(containerID)
	write(netns)
	write(ifname)
	write(podNS)
	write(podName)
	write(podUID)
	sum := h.Sum(nil)
	// Return 128-bit hex (shorter, sufficient for dedup) to keep the key compact.
	return hex.EncodeToString(sum[:16])
}
