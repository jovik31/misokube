package daemon

import (
	"encoding/json"
	"fmt"
	socketserver "github/setera/pkg/server"
	"log"
	"net/http"
)

type CNIRequest struct {
	Command     string            `json:"command"`
	ContainerID string            `json:"containerID"`
	NetNS       string            `json:"netNS"`
	Args        map[string]string `json:"args"`
	Ifname      string            `json:"ifname"`
	PodName     string            `json:"podName"`
}

type CNIResponse struct {
	IP      string `json:"ip"`
	Gateway string `json:"gateway"`
	Error   string `json:"error,omitempty"`
}

func HandleCNI(w http.ResponseWriter, r *http.Request) {
	var req CNIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	resp := CNIResponse{}
	switch req.Command {
	case "ADD":
		// Placeholder for actual network logic
		log.Printf("Handling ADD for container %s in pod %s", req.ContainerID, req.PodName)
		resp.IP = "10.42.0.10"
		resp.Gateway = "10.42.0.1"
	case "DEL":
		// cleanup logic here
	case "CHECK":
		// health check logic here
	default:
		resp.Error = "unsupported command"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func NewCNIServer(socketPath string) error {

	// init socket server
	s, err := socketserver.NewServer(socketPath)
	if err != nil {
		return err
	}
	s.Register("/CNI", HandleCNI)
	fmt.Printf("Starting CNI server on %s\n", socketPath)
	if err := s.Start(); err != nil {
		return err
	}
	return nil
}
