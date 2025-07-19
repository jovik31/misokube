package routing

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"syscall"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// Config for discovering the default *egress* interface.
type DefaultRouteLookupConfig struct {
	AllowIPv6Fallback bool     // if true, try IPv6 if no IPv4 default found
	OnlyMainTable     bool     // if true, restrict to RT_TABLE_MAIN (254)
	ExcludeIfaces     []string // exact interface names to skip (e.g. "lo")
	ExcludePrefixes   []string // interface name prefixes to skip (e.g. "docker", "cni", "veth", "br-")
	PreferNonVirtual  bool     // attempt to de-prioritize bridges/loops (heuristic)
}

// Result of the lookup.
type DefaultRouteInfo struct {
	Interface *net.Interface
	Gateway   net.IP // May be nil if not set (e.g. directly connected default)
	Family    int    // syscall.AF_INET or syscall.AF_INET6
	Table     int
}

// High-level convenience (IPv4 only, default config).
func GetDefaultGatewayInterface() (*net.Interface, net.IP, error) {
	res, err := FindDefaultRoute(DefaultRouteLookupConfig{
		AllowIPv6Fallback: false,
		OnlyMainTable:     true,
		ExcludeIfaces:     []string{"lo"},
		ExcludePrefixes:   []string{"docker", "cni", "veth", "br-"},
		PreferNonVirtual:  true,
	})
	if err != nil {
		return nil, nil, err
	}
	return res.Interface, res.Gateway, nil
}

// Full-featured finder.
func FindDefaultRoute(cfg DefaultRouteLookupConfig) (*DefaultRouteInfo, error) {
	// Try IPv4 first
	v4, err := gatherDefaultRoutes(syscall.AF_INET, cfg)
	if err == nil && len(v4) > 0 {
		chosen := chooseRoute(v4, cfg)
		return routeToInfo(chosen, syscall.AF_INET), nil
	}
	if err != nil && !errors.Is(err, ErrNoDefaultRoute) {
		return nil, fmt.Errorf("ipv4 route lookup error: %w", err)
	}

	if cfg.AllowIPv6Fallback {
		v6, err6 := gatherDefaultRoutes(syscall.AF_INET6, cfg)
		if err6 == nil && len(v6) > 0 {
			chosen := chooseRoute(v6, cfg)
			return routeToInfo(chosen, syscall.AF_INET6), nil
		}
		if err6 != nil && !errors.Is(err6, ErrNoDefaultRoute) {
			return nil, fmt.Errorf("ipv6 route lookup error: %w", err6)
		}
	}

	return nil, ErrNoDefaultRoute
}

// ------------------------------------------------------------
// Implementation details
// ------------------------------------------------------------

var ErrNoDefaultRoute = errors.New("no suitable default route found")

type routeWithLink struct {
	Route netlink.Route
	Link  netlink.Link
}

// get all default routes for a family (filtered).
func gatherDefaultRoutes(family int, cfg DefaultRouteLookupConfig) ([]routeWithLink, error) {
	routes, err := netlink.RouteListFiltered(family, &netlink.Route{}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return nil, fmt.Errorf("RouteListFiltered: %w", err)
	}

	var result []routeWithLink
	for _, r := range routes {
		// Filter by table if requested
		if cfg.OnlyMainTable && r.Table != unix.RT_TABLE_MAIN {
			continue
		}

		// Identify default route:
		// For netlink, default = r.Dst == nil
		if r.Dst != nil {
			continue
		}

		// Multipath route variant (contains RTA_MULTIPATH)
		if len(r.MultiPath) > 0 {
			for _, nh := range r.MultiPath {
				if nh.LinkIndex <= 0 {
					continue
				}
				link, lerr := netlink.LinkByIndex(nh.LinkIndex)
				if lerr != nil {
					continue
				}
				if skipLink(link, cfg) {
					continue
				}
				result = append(result, routeWithLink{
					Route: netlink.Route{
						LinkIndex: nh.LinkIndex,
						Gw:        nh.Gw,
						Table:     r.Table,
					},
					Link: link,
				})
			}
			continue
		}

		if r.LinkIndex <= 0 {
			continue
		}
		link, lerr := netlink.LinkByIndex(r.LinkIndex)
		if lerr != nil {
			continue
		}
		if skipLink(link, cfg) {
			continue
		}
		result = append(result, routeWithLink{Route: r, Link: link})
	}

	if len(result) == 0 {
		return nil, ErrNoDefaultRoute
	}
	return result, nil
}

// Heuristic filter for links
func skipLink(l netlink.Link, cfg DefaultRouteLookupConfig) bool {
	name := l.Attrs().Name
	for _, ex := range cfg.ExcludeIfaces {
		if name == ex {
			return true
		}
	}
	for _, p := range cfg.ExcludePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// Choose “best” route among candidates.
func chooseRoute(routes []routeWithLink, cfg DefaultRouteLookupConfig) routeWithLink {
	if len(routes) == 1 || !cfg.PreferNonVirtual {
		return routes[0]
	}

	// Rank: prefer non-bridge, non-veth, non-docker first
	type ranked struct {
		r    routeWithLink
		rank int
	}
	var rankedList []ranked
	for _, rw := range routes {
		rank := 100
		typ := rw.Link.Type()
		name := rw.Link.Attrs().Name
		// Improve ranking heuristics
		if !strings.Contains(typ, "bridge") &&
			!strings.HasPrefix(name, "br-") &&
			!strings.HasPrefix(name, "docker") &&
			!strings.HasPrefix(name, "veth") &&
			!strings.HasPrefix(name, "cni") {
			rank = 0
		}
		rankedList = append(rankedList, ranked{r: rw, rank: rank})
	}

	sort.Slice(rankedList, func(i, j int) bool {
		return rankedList[i].rank < rankedList[j].rank
	})
	return rankedList[0].r
}

func routeToInfo(r routeWithLink, family int) *DefaultRouteInfo {
	iface, _ := net.InterfaceByIndex(r.Link.Attrs().Index)
	return &DefaultRouteInfo{
		Interface: iface,
		Gateway:   r.Route.Gw,
		Family:    family,
		Table:     r.Route.Table,
	}
}
