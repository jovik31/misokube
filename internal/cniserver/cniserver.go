package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"

	cnitypes "github.com/containernetworking/cni/pkg/types"
	types100 "github.com/containernetworking/cni/pkg/types/100"

	"github/setera/internal/resolver"
	"github/setera/pkg/transport/uds"
	"github/setera/pkg/wire"
)

// CNIServer handles concurrent CNI requests and serializes work per-tenant via Router.
type CNIServer struct {
	// Path to the Unix domain socket to bind.
	socketPath string

	// Deps
	resolver resolver.Resolver
	router   router.Router
	// Default CNI version used when requests don't specify one.
	defaultCNIVersion string

	// internal state
	cache   map[string]podCacheEntry
	cacheMu sync.RWMutex
}

type podCacheEntry struct {
	result  types100.Result
	lastCmd wire.Command
	idemKey string
	updated time.Time
}

func (s *CNIServer) cacheStore(uid string, cmd wire.Command, idemKey string, res *types100.Result) {
	if uid == "" {
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	var stored types100.Result
	if res != nil {
		stored = *res
	}
	s.cache[uid] = podCacheEntry{
		result:  stored,
		lastCmd: cmd,
		idemKey: idemKey,
		updated: time.Now(),
	}
}

func (s *CNIServer) cacheGet(uid string) (podCacheEntry, error) {
	var entry podCacheEntry
	if uid == "" {
		return entry, errors.New("cache disabled")
	}
	s.cacheMu.RLock()
	entry, found := s.cache[uid]
	s.cacheMu.RUnlock()
	if !found {
		return entry, errors.New("cache miss")
	}
	if time.Since(entry.updated) > 10*time.Minute {
		s.cacheMu.Lock()
		delete(s.cache, uid)
		s.cacheMu.Unlock()
		return entry, errors.New("cache expired")
	}
	return entry, nil
}

func (s *CNIServer) cacheTouch(uid string) {
	if uid == "" {
		return
	}
	s.cacheMu.Lock()
	if entry, ok := s.cache[uid]; ok {
		entry.updated = time.Now()
		s.cache[uid] = entry
	}
	s.cacheMu.Unlock()
}

const fallbackCNIVersion = "1.1.0"

func NewCNIServer(socketPath string, r resolver.Resolver, defaultVersion string) *CNIServer {
	if socketPath == "" {
		log.Panic("cniserver: missing socket path")
	}
	if defaultVersion == "" {
		defaultVersion = fallbackCNIVersion
	}
	return &CNIServer{
		socketPath:        socketPath,
		resolver:          r,
		defaultCNIVersion: defaultVersion,
		cache:             make(map[string]podCacheEntry),
	}
}

// SetRouter attaches a Router to the server. Optional.
func (s *CNIServer) SetRouter(rt router.Router) { s.router = rt }

// Run is a convenience wrapper over Start that begins serving in the background
// and returns immediately (non-blocking). It logs the socket path on success.
func (s *CNIServer) Run() error {
	_, err := s.Start()
	return err
}

// Start binds the UDS and starts serving requests concurrently.
func (s *CNIServer) Start() (net.Listener, error) {
	ln, err := uds.Listen(s.socketPath)
	if err != nil {
		return nil, err
	}
	go uds.ServeLoop(ln, s.handleConn)
	log.Printf("cniserver listening on %s", s.socketPath)
	return ln, nil
}

func (s *CNIServer) handleConn(c net.Conn) {
	defer c.Close()
	var req wire.Request
	hdr, err := uds.ReadRequest(c, &req)
	if err != nil {
		// Ignore benign EOFs from short-lived clients/probes
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			log.Printf("read request: %v", err)
		}
		return
	}

	//start := time.Now()
	//s.logRequest(&req)

	// Handle per-command behavior with minimal responses.
	var (
		resp *wire.Response
		//cacheEvent = "none"
		storeEntry bool
		storeRes   *types100.Result
	)
	switch req.Cmd {
	case wire.CmdSTATUS:
		resp = s.handleStatusRequest(&req)
		//cacheEvent = "skip"
	case wire.CmdGC:
		resp = s.handleGCRequest(&req)
		//cacheEvent = "skip"
	default:
		tenantID, resolveErr := s.resolveTenantForRequest(&req)
		if resolveErr != nil {
			resp = &wire.Response{
				OK:      false,
				Message: "resolve tenant: " + resolveErr.Error(),
				Result:  s.encodeResult(req.CNIVersion, nil),
			}
			break
		}
		start := time.Now()
		var ce string
		resp, storeRes, storeEntry, ce = s.handleTenantCommand(&req, tenantID)
		dur := time.Since(start)
		if resp != nil {
			podKey := req.PodNamespace + "/" + req.PodName
			if podKey == "/" {
				podKey = "-"
			}
			if resp.OK {
				log.Printf("[CNI][%s] ok tenant=%s pod=%s dur=%s", req.Cmd, tenantID, podKey, dur)
			} else {
				log.Printf("[CNI][%s] error tenant=%s pod=%s dur=%s msg=%s", req.Cmd, tenantID, podKey, dur, resp.Message)
			}
		}
		if ce != "" {
			//cacheEvent = ce
		}
	}

	if resp != nil && resp.OK && storeEntry {
		s.cacheStore(req.PodUID, req.Cmd, req.IdemKey, storeRes)
	}

	_ = uds.WriteResponse(c, hdr.Cmd, hdr.Flags, resp)
	//s.logResponse(&req, resp, cacheEvent, time.Since(start))
}

func (s *CNIServer) handleStatusRequest(req *wire.Request) *wire.Response {
	return &wire.Response{
		OK:      true,
		Message: "ready",
		Result:  s.encodeResult(req.CNIVersion, nil),
	}
}

func (s *CNIServer) handleGCRequest(req *wire.Request) *wire.Response {
	return &wire.Response{
		OK:      true,
		Message: "gc ok",
		Result:  s.encodeResult(req.CNIVersion, nil),
	}
}

func (s *CNIServer) handleTenantCommand(req *wire.Request, tenantID string) (*wire.Response, *types100.Result, bool, string) {
	switch req.Cmd {
	case wire.CmdADD:
		if entry, err := s.cacheGet(req.PodUID); err == nil && entry.lastCmd == wire.CmdADD && entry.idemKey != "" && entry.idemKey == req.IdemKey {
			s.cacheTouch(req.PodUID)
			resCopy := entry.result
			return &wire.Response{
				OK:      true,
				Message: "add ok (cached)",
				Result:  s.encodeResult(req.CNIVersion, &resCopy),
			}, nil, false, "hit"
		}
		ver := req.CNIVersion
		if ver == "" {
			ver = s.defaultCNIVersion
		}
		var podResult *types100.Result
		if s.router != nil {
			pa := buildPodAttachArgs(req)
			ctx := context.Background()
			if req.TimeoutSeconds > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutSeconds)*time.Second)
				defer cancel()
			}
			var err error
			podResult, err = s.router.ConfigurePod(ctx, tenantID, req.PodUID, pa)
			if err != nil {
				return &wire.Response{
					OK:      false,
					Message: "configure pod: " + err.Error(),
					Result:  s.encodeResult(ver, podResult),
				}, nil, false, "error"
			}
		}
		podResult = ensureResultStruct(ver, podResult)
		return &wire.Response{
			OK:      true,
			Message: "add ok for tenant:" + tenantID,
			Result:  s.encodeResult(ver, podResult),
		}, podResult, true, "store"

	case wire.CmdDEL:
		if entry, err := s.cacheGet(req.PodUID); err == nil && entry.lastCmd == wire.CmdDEL && entry.idemKey != "" && entry.idemKey == req.IdemKey {
			s.cacheTouch(req.PodUID)
			return &wire.Response{
				OK:      true,
				Message: "del ok (cached)",
				Result:  s.encodeResult(req.CNIVersion, nil),
			}, nil, false, "hit"
		}
		if s.router != nil {
			pa := buildPodAttachArgs(req)
			ctx := context.Background()
			if req.TimeoutSeconds > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutSeconds)*time.Second)
				defer cancel()
			}
			if err := s.router.RemovePod(ctx, tenantID, req.PodUID, pa); err != nil {
				return &wire.Response{
					OK:      false,
					Message: "remove pod: " + err.Error(),
					Result:  s.encodeResult(req.CNIVersion, nil),
				}, nil, false, "error"
			}
		}
		return &wire.Response{
			OK:      true,
			Message: "del ok",
			Result:  s.encodeResult(req.CNIVersion, nil),
		}, nil, true, "store-del"
	case wire.CmdCHECK:
		if entry, err := s.cacheGet(req.PodUID); err == nil && entry.lastCmd == wire.CmdADD {
			s.cacheTouch(req.PodUID)
			resCopy := entry.result
			return &wire.Response{
				OK:      true,
				Message: "check ok (cached)",
				Result:  s.encodeResult(req.CNIVersion, &resCopy),
			}, nil, false, "hit"
		}
		return &wire.Response{
			OK:      true,
			Message: "check ok",
			Result:  s.encodeResult(req.CNIVersion, nil),
		}, nil, false, "miss"
	default:
		return &wire.Response{OK: false, Message: "unknown cmd"}, nil, false, "none"
	}
}

func (s *CNIServer) resolveTenantForRequest(req *wire.Request) (string, error) {
	switch req.Cmd {
	case wire.CmdADD, wire.CmdCHECK, wire.CmdDEL:
	default:
		return "", nil
	}
	if s.resolver == nil {
		return "", errors.New("resolver not configured")
	}
	if !s.resolver.Ready() {
		return "", errors.New("resolver pending")
	}
	tenantID, pending, err := s.resolver.Resolve(req.PodUID)
	if err != nil {
		return "", err
	}
	if pending {
		return "", errors.New("resolver pending")
	}
	return tenantID, nil
}

func buildPodAttachArgs(req *wire.Request) router.PodAttachArgs {
	return router.PodAttachArgs{
		Namespace:   req.PodNamespace,
		PodName:     req.PodName,
		ContainerID: req.ContainerID,
		NetNS:       req.NetNS,
		IfName:      req.IfName,
	}
}

func ensureResultStruct(ver string, res *types100.Result) *types100.Result {
	if res == nil {
		return &types100.Result{CNIVersion: ver}
	}
	if res.CNIVersion == "" {
		res.CNIVersion = ver
	}
	return res
}

func (s *CNIServer) encodeResult(ver string, res cnitypes.Result) []byte {
	target := ver
	if target == "" {
		target = s.defaultCNIVersion
	}
	switch typed := res.(type) {
	case nil:
		res = &types100.Result{CNIVersion: target}
	case *types100.Result:
		if typed == nil {
			res = &types100.Result{CNIVersion: target}
		}
	}
	if res == nil {
		res = &types100.Result{CNIVersion: target}
	}
	converted, err := res.GetAsVersion(target)
	if err != nil {
		converted = &types100.Result{CNIVersion: target}
	}
	data, err := json.Marshal(converted)
	if err != nil {
		return []byte("{}")
	}
	return data
}

// No resolver integration in the stub: keep behavior minimal.
