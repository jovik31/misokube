package wire

import "encoding/json"

type Command string

const (
	CmdADD    Command = "ADD"
	CmdDEL    Command = "DEL"
	CmdCHECK  Command = "CHECK"
	CmdSTATUS Command = "STATUS"
	CmdGC     Command = "GC"
)

// for the UDS transport we also add the CMD to the header
type Request struct {
	Cmd            Command `json:"cmd"`
	TraceID        string  `json:"trace_id"`
	CNIVersion     string  `json:"cniVersion"`
	ContainerID    string  `json:"container_id"`
	NetNS          string  `json:"netns"`
	IfName         string  `json:"ifname"`
	PodName        string  `json:"pod_name,omitempty"`
	PodNamespace   string  `json:"pod_namespace,omitempty"`
	PodUID         string  `json:"pod_uid,omitempty"`
	TimeoutSeconds int     `json:"timeout_seconds"`
	IdemKey        string  `json:"idem_key"`
}

type Response struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	// For ADD: a CNI current.Result JSON blob
	Result json.RawMessage `json:"result,omitempty"`
}
