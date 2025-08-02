package cni

import (
	"encoding/json"
	"io"
)

func writeResult(ip, gw string, w io.Writer) error {

	result := map[string]interface{}{

		"cniVersion": "0.4.0",
		"ip": []map[string]interface{}{
			{
				"version": "4",
				"ip":      ip + "/32",
				"gateway": gw,
			},
		},
		"interfaces": []map[string]interface{}{
			{
				"name": "eth0",
			},
		},
	}
	return json.NewEncoder(w).Encode(result)
}
