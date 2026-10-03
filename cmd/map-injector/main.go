package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"syscall"

	"github.com/cilium/ebpf"
)

const defaultMapPath = "/sys/fs/bpf/misokube/tc/tc_podIDs"

type podValue struct {
	Tenant      [64]byte
	VethIfindex uint32
}

type report struct {
	MapPath   string `json:"map_path"`
	Capacity  uint32 `json:"capacity"`
	Target    int    `json:"target,omitempty"`
	Real      int    `json:"real_entries"`
	Synthetic int    `json:"synthetic_entries"`
	Total     int    `json:"total_entries"`
	Operation string `json:"operation"`
}

func keyBytes(key uint32) [4]byte {
	var raw [4]byte
	binary.NativeEndian.PutUint32(raw[:], key)
	return raw
}

func syntheticKey(key uint32) bool {
	raw := keyBytes(key)
	return raw[0] == 198 && (raw[1] == 18 || raw[1] == 19)
}

func makeKey(index int) uint32 {
	third := byte(index >> 8)
	fourth := byte(index)
	second := byte(18 + ((index >> 16) & 1))
	raw := [4]byte{198, second, third, fourth}
	return binary.NativeEndian.Uint32(raw[:])
}

func scan(m *ebpf.Map) (real int, synthetic []uint32, err error) {
	iterator := m.Iterate()
	var key uint32
	var value podValue
	for iterator.Next(&key, &value) {
		if syntheticKey(key) {
			synthetic = append(synthetic, key)
		} else {
			real++
		}
	}
	return real, synthetic, iterator.Err()
}

func main() {
	mapPath := flag.String("map-path", defaultMapPath, "pinned tc_podIDs path")
	target := flag.Int("target", -1, "desired total entry count")
	cleanup := flag.Bool("cleanup", false, "remove all synthetic benchmark entries")
	flag.Parse()

	if *target < 0 && !*cleanup {
		fmt.Fprintln(os.Stderr, "one of --target or --cleanup is required")
		os.Exit(2)
	}

	m, err := ebpf.LoadPinnedMap(*mapPath, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open pinned map: %v\n", err)
		os.Exit(1)
	}
	defer m.Close()
	info, err := m.Info()
	if err != nil {
		fmt.Fprintf(os.Stderr, "map info: %v\n", err)
		os.Exit(1)
	}

	real, synthetic, err := scan(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scan map: %v\n", err)
		os.Exit(1)
	}
	operation := "inspect"

	if *cleanup {
		operation = "cleanup"
		for _, key := range synthetic {
			if err := m.Delete(key); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
				fmt.Fprintf(os.Stderr, "delete synthetic key: %v\n", err)
				os.Exit(1)
			}
		}
	} else {
		operation = "ensure-target"
		if *target > int(info.MaxEntries) {
			fmt.Fprintf(os.Stderr, "target %d exceeds capacity %d\n", *target, info.MaxEntries)
			os.Exit(1)
		}
		if real > *target {
			fmt.Fprintf(os.Stderr, "real entries %d exceed target %d\n", real, *target)
			os.Exit(1)
		}
		desiredSynthetic := *target - real
		if len(synthetic) > desiredSynthetic {
			for _, key := range synthetic[desiredSynthetic:] {
				if err := m.Delete(key); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
					fmt.Fprintf(os.Stderr, "trim synthetic key: %v\n", err)
					os.Exit(1)
				}
			}
		}
		if len(synthetic) < desiredSynthetic {
			var value podValue
			copy(value.Tenant[:], "benchmark-dummy")
			value.VethIfindex = math.MaxUint32
			for index := 0; len(synthetic) < desiredSynthetic && index < 131072; index++ {
				key := makeKey(index)
				err := m.Update(key, value, ebpf.UpdateNoExist)
				if errors.Is(err, syscall.EEXIST) {
					continue
				}
				if err != nil {
					fmt.Fprintf(os.Stderr, "insert synthetic key: %v\n", err)
					os.Exit(1)
				}
				synthetic = append(synthetic, key)
			}
		}
	}

	real, synthetic, err = scan(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify map: %v\n", err)
		os.Exit(1)
	}
	result := report{
		MapPath: *mapPath, Capacity: info.MaxEntries, Target: *target,
		Real: real, Synthetic: len(synthetic), Total: real + len(synthetic),
		Operation: operation,
	}
	if *target >= 0 && result.Total != *target {
		fmt.Fprintf(os.Stderr, "verification failed: total=%d target=%d\n", result.Total, *target)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "encode report: %v\n", err)
		os.Exit(1)
	}
}
