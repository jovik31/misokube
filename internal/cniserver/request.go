package cniserver

import (
	"context"
	"fmt"

	"github/setera/internal/podnetwork"
	"github/setera/pkg/tenantmeta"
	"github/setera/pkg/wire"
)

func (s *Server) handleRequest(ctx context.Context, req *wire.Request) *wire.Response {
	if req == nil {
		return failure("invalid request: request is nil")
	}

	switch req.Cmd {
	case wire.CmdADD:
		return s.handleAdd(ctx, req)
	case wire.CmdDEL:
		return s.handleDel(ctx, req)
	case wire.CmdCHECK:
		return s.handleCheck(ctx, req)
	case wire.CmdSTATUS:
		return success("ready")
	case wire.CmdGC:
		return success("gc ok")
	default:
		return failure(fmt.Sprintf("unsupported CNI command %q", req.Cmd))
	}
}

func (s *Server) handleAdd(ctx context.Context, req *wire.Request) *wire.Response {
	if err := validateAddRequest(req); err != nil {
		return failure(err.Error())
	}
	if err := ctx.Err(); err != nil {
		return failure(err.Error())
	}

	pod, err := s.pods.Pods(req.PodNamespace).Get(req.PodName)
	if err != nil {
		return failure(fmt.Sprintf("get pod %s/%s: %v", req.PodNamespace, req.PodName, err))
	}

	if string(pod.UID) != req.PodUID {
		return failure(fmt.Sprintf(
			"pod UID mismatch for %s/%s: got %s, want %s",
			req.PodNamespace,
			req.PodName,
			req.PodUID,
			pod.UID,
		))
	}

	tenantID := tenantmeta.ResolvePodTenant(
		pod.Namespace,
		pod.Labels,
	)

	result, err := s.podNetwork.AddPod(ctx, podnetwork.Request{
		ContainerID: req.ContainerID,
		NetNS:       req.NetNS,
		IfName:      req.IfName,
		PodUID:      req.PodUID,
		TenantID:    tenantID,
	})
	if err != nil {
		return failure(fmt.Sprintf("add pod network: %v", err))
	}

	encoded, err := encodeAddResult(
		s.defaultCNIVersion,
		req.CNIVersion,
		req,
		result,
	)
	if err != nil {
		return failure(fmt.Sprintf("encode CNI result: %v", err))
	}

	return &wire.Response{
		OK:      true,
		Message: "add ok",
		Result:  encoded,
	}
}

func (s *Server) handleDel(ctx context.Context, req *wire.Request) *wire.Response {
	if err := validateDeleteRequest(req); err != nil {
		return failure(err.Error())
	}
	if err := ctx.Err(); err != nil {
		return failure(err.Error())
	}

	// DEL does not read the Pod lister. The Pod can already be deleted when CNI
	// DEL runs. ContainerID + IfName identify the NodeIPAM allocation.
	if err := s.podNetwork.DelPod(ctx, podnetwork.Request{
		ContainerID: req.ContainerID,
		NetNS:       req.NetNS,
		IfName:      req.IfName,
	}); err != nil {
		return failure(fmt.Sprintf("delete pod network: %v", err))
	}

	return success("del ok")
}

func (s *Server) handleCheck(ctx context.Context, req *wire.Request) *wire.Response {
	if err := validateCheckRequest(req); err != nil {
		return failure(err.Error())
	}
	if err := ctx.Err(); err != nil {
		return failure(err.Error())
	}

	if err := s.podNetwork.CheckPod(ctx, podnetwork.Request{
		ContainerID: req.ContainerID,
		NetNS:       req.NetNS,
		IfName:      req.IfName,
	}); err != nil {
		return failure(fmt.Sprintf("check pod network: %v", err))
	}

	return success("check ok")
}

func validateAddRequest(req *wire.Request) error {
	if err := validateAttachment(req); err != nil {
		return err
	}
	if req.NetNS == "" {
		return fmt.Errorf("invalid ADD request: network namespace is empty")
	}
	if req.PodNamespace == "" {
		return fmt.Errorf("invalid ADD request: pod namespace is empty")
	}
	if req.PodName == "" {
		return fmt.Errorf("invalid ADD request: pod name is empty")
	}
	if req.PodUID == "" {
		return fmt.Errorf("invalid ADD request: pod UID is empty")
	}
	return nil
}

func validateDeleteRequest(req *wire.Request) error {
	return validateAttachment(req)
}

func validateCheckRequest(req *wire.Request) error {
	if err := validateAttachment(req); err != nil {
		return err
	}
	if req.NetNS == "" {
		return fmt.Errorf("invalid CHECK request: network namespace is empty")
	}
	return nil
}

func validateAttachment(req *wire.Request) error {
	if req.ContainerID == "" {
		return fmt.Errorf("invalid request: container ID is empty")
	}
	if req.IfName == "" {
		return fmt.Errorf("invalid request: interface name is empty")
	}
	return nil
}

func success(message string) *wire.Response {
	return &wire.Response{
		OK:      true,
		Message: message,
	}
}

func failure(message string) *wire.Response {
	return &wire.Response{
		OK:      false,
		Message: message,
	}
}
