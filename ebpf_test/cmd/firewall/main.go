package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github/setera/ebpf-test/pkg/firewall"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// RuleConfig represents a single rule in the JSON rules file.
type RuleConfig struct {
	SrcIP     string `json:"srcIP,omitempty"`
	DstIP     string `json:"dstIP,omitempty"`
	SrcPort   int    `json:"srcPort,omitempty"`
	DstPort   int    `json:"dstPort,omitempty"`
	SrcPrefix int    `json:"srcPrefix,omitempty"` // CIDR prefix length for source IP
	DstPrefix int    `json:"dstPrefix,omitempty"` // CIDR prefix length for destination IP
	Protocol  string `json:"protocol,omitempty"`  // tcp, udp, icmp
	Action    string `json:"action"`              // allow, drop, log
}

func main() {
	mode := flag.String("mode", "xdp", "Firewall mode: xdp or tc")
	iface := flag.String("iface", "eth0", "Network interface to attach to")
	defaultAction := flag.String("default", "allow", "Default action: allow or drop")
	conntrack := flag.Bool("conntrack", false, "Enable connection tracking (TC mode only)")
	cmName := flag.String("configmap", "firewall-rules", "ConfigMap name to watch for rule updates")
	cmNamespace := flag.String("configmap-ns", "default", "Namespace of the ConfigMap to watch")
	cmKey := flag.String("configmap-key", "rules.json", "Key inside the ConfigMap that contains the rules JSON")

	flag.Parse()

	fmt.Printf("Setera eBPF Firewall Test\n")
	fmt.Printf("  Mode:           %s\n", *mode)
	fmt.Printf("  Interface:      %s\n", *iface)
	fmt.Printf("  Default action: %s\n", *defaultAction)

	defAction := firewall.ActionAllow
	if *defaultAction == "drop" {
		defAction = firewall.ActionDrop
	}

	// syncRules will be set once the firewall is created
	var syncRules func([]firewall.Rule) error

	switch *mode {
	case "xdp":
		fw, err := firewall.NewXDPFirewall(*iface)
		if err != nil {
			fmt.Fprintf(os.Stderr, "XDP error: %v\n", err)
			os.Exit(1)
		}
		defer fw.Close()
		if err := fw.SetDefaultAction(defAction); err != nil {
			fmt.Fprintf(os.Stderr, "set default action: %v\n", err)
			os.Exit(1)
		}
		syncRules = fw.SyncRules
		fmt.Printf("\nXDP firewall attached to %s.\n", *iface)

	case "tc":
		fw, err := firewall.NewTCFirewall(*iface)
		if err != nil {
			fmt.Fprintf(os.Stderr, "TC error: %v\n", err)
			os.Exit(1)
		}
		defer fw.Close()
		if err := fw.SetDefaultAction(defAction, *conntrack); err != nil {
			fmt.Fprintf(os.Stderr, "set default action: %v\n", err)
			os.Exit(1)
		}
		syncRules = fw.SyncRules
		fmt.Printf("\nTC firewall attached to %s (ingress+egress).\n", *iface)

	default:
		fmt.Fprintf(os.Stderr, "Unknown mode %q, use xdp or tc\n", *mode)
		os.Exit(1)
	}

	// Start ConfigMap watcher (informer)
	stopInformer := make(chan struct{})
	defer close(stopInformer)

	config, err := rest.InClusterConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get in-cluster config: %v\n", err)
		os.Exit(1)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create k8s client: %v\n", err)
		os.Exit(1)
	}

	watchlist := cache.NewListWatchFromClient(
		clientset.CoreV1().RESTClient(),
		"configmaps",
		*cmNamespace,
		fields.OneTermEqualSelector("metadata.name", *cmName),
	)
	dataKey := *cmKey
	loadFromCM := func(cm *corev1.ConfigMap) {
		fmt.Printf("  ConfigMap %q changed, reloading rules...\n", cm.Name)
		newRules, err := parseRulesData([]byte(cm.Data[dataKey]))
		if err != nil {
			fmt.Printf("  [rules parse error: %v]\n", err)
			return
		}
		if err := syncRules(newRules); err != nil {
			fmt.Printf("  [rules sync error: %v]\n", err)
			return
		}
		fmt.Printf("  Rules reloaded: %d rules from ConfigMap\n", len(newRules))
	}
	_, controller := cache.NewInformer(
		watchlist,
		&corev1.ConfigMap{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				loadFromCM(obj.(*corev1.ConfigMap))
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				loadFromCM(newObj.(*corev1.ConfigMap))
			},
		},
	)
	go controller.Run(stopInformer)
	fmt.Printf("  Watching ConfigMap %s/%s (key %q) for rule changes\n", *cmNamespace, *cmName, dataKey)

	// Block on signal + stats
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sig:
			fmt.Println("\nDetaching firewall...")
			return
		case <-ticker.C:
		}
	}
}

func parseRulesData(data []byte) ([]firewall.Rule, error) {
	var configs []RuleConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, fmt.Errorf("parse rules: %w", err)
	}

	var rules []firewall.Rule
	for _, c := range configs {
		r := buildRule(c.SrcIP, c.DstIP, c.SrcPort, c.DstPort, c.Protocol, c.Action, c.SrcPrefix, c.DstPrefix)
		if r != nil {
			rules = append(rules, *r)
		}
	}
	return rules, nil
}

func buildRule(srcIP, dstIP string, srcPort, dstPort int, proto, action string, srcPrefix, dstPrefix int) *firewall.Rule {
	if srcIP == "" && dstIP == "" && srcPort == 0 && dstPort == 0 && proto == "" {
		return nil
	}

	r := &firewall.Rule{
		SrcPort: uint16(srcPort),
		DstPort: uint16(dstPort),
	}

	if srcIP != "" {
		r.SrcIP = net.ParseIP(srcIP)
		r.SrcPrefix = uint8(srcPrefix)
	}
	if dstIP != "" {
		r.DstIP = net.ParseIP(dstIP)
		r.DstPrefix = uint8(dstPrefix)
	}

	switch proto {
	case "tcp":
		r.Protocol = syscall.IPPROTO_TCP
	case "udp":
		r.Protocol = syscall.IPPROTO_UDP
	case "icmp":
		r.Protocol = syscall.IPPROTO_ICMP
	}

	switch action {
	case "allow":
		r.Action = firewall.ActionAllow
	case "drop":
		r.Action = firewall.ActionDrop
	case "log":
		r.Action = firewall.ActionLog
	default:
		r.Action = firewall.ActionDrop
	}

	return r
}

func ruleString(r firewall.Rule) string {
	actionStr := "drop"
	switch r.Action {
	case firewall.ActionAllow:
		actionStr = "allow"
	case firewall.ActionLog:
		actionStr = "log"
	}

	src := "any"
	if r.SrcIP != nil {
		src = r.SrcIP.String()
	}
	dst := "any"
	if r.DstIP != nil {
		dst = r.DstIP.String()
	}
	proto := "any"
	switch r.Protocol {
	case syscall.IPPROTO_TCP:
		proto = "tcp"
	case syscall.IPPROTO_UDP:
		proto = "udp"
	case syscall.IPPROTO_ICMP:
		proto = "icmp"
	}

	return fmt.Sprintf("%s -> %s proto=%s srcPort=%d dstPort=%d action=%s",
		src, dst, proto, r.SrcPort, r.DstPort, actionStr)
}
