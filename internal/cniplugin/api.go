package cniplugin

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/containernetworking/cni/pkg/skel"

	"github/setera/pkg/transport/uds"
	"github/setera/pkg/wire"
)

type Options struct {
	SocketPath string
	Timeout    time.Duration
}

func Add(args *skel.CmdArgs, out io.Writer, opt Options) error {

	if err := validateArgs(args); err != nil {
		return err
	}
	if err := checkReadiness(opt); err != nil {
		return fmt.Errorf("daemon not ready: %w", err)
	}

	pod_name := get_pod_name_regex(args.Args)
	pod_namespace := get_pod_namespace_regex(args.Args)
	pod_uid := get_pod_uid_regex(args.Args)

	req := wire.Request{
		Cmd:            wire.CmdADD,
		ContainerID:    args.ContainerID,
		NetNS:          args.Netns,
		IfName:         args.IfName,
		PodName:        pod_name,
		PodNamespace:   pod_namespace,
		PodUID:         pod_uid,
		TimeoutSeconds: int(opt.Timeout.Seconds()),
	}
	req.IdemKey = computeIdemKey(req.Cmd, req.ContainerID, req.NetNS, req.IfName, req.PodNamespace, req.PodName, req.PodUID)

	logCNIRequest(&req)
	var resp wire.Response
	if err := uds.NewClientJSON().Call(opt.SocketPath, opt.Timeout, &req, &resp); err != nil {
		log.Printf("[CNI][%s] transport error: %v", req.Cmd, err)
		return err
	}
	logCNIResponse(&req, &resp)
	if !resp.OK {
		return errors.New(nonEmpty(resp.Message, "daemon error"))
	}

	if len(resp.Result) == 0 {
		ver := detectCNIVersion(args.StdinData)
		_, _ = out.Write([]byte(`{"cniVersion":"` + ver + `","interfaces":[],"ips":[],"routes":[]}`))
		return nil
	}
	_, err := out.Write(resp.Result)
	return err
}

func Check(args *skel.CmdArgs, out io.Writer, opt Options) error {

	if err := validateArgs(args); err != nil {
		return err
	}
	if err := checkReadiness(opt); err != nil {
		return fmt.Errorf("daemon not ready: %w", err)
	}

	pod_name := get_pod_name_regex(args.Args)
	pod_namespace := get_pod_namespace_regex(args.Args)
	pod_uid := get_pod_uid_regex(args.Args)

	req := wire.Request{
		Cmd:            wire.CmdCHECK,
		ContainerID:    args.ContainerID,
		NetNS:          args.Netns,
		IfName:         args.IfName,
		PodName:        pod_name,
		PodNamespace:   pod_namespace,
		PodUID:         pod_uid,
		TimeoutSeconds: int(opt.Timeout.Seconds()),
	}
	req.IdemKey = computeIdemKey(req.Cmd, req.ContainerID, req.NetNS, req.IfName, req.PodNamespace, req.PodName, req.PodUID)
	var resp wire.Response
	logCNIRequest(&req)
	if err := uds.NewClientJSON().Call(opt.SocketPath, opt.Timeout, &req, &resp); err != nil {
		log.Printf("[CNI][%s] transport error: %v", req.Cmd, err)
		return err
	}
	logCNIResponse(&req, &resp)
	if !resp.OK {
		return errors.New(nonEmpty(resp.Message, "daemon error"))
	}
	if len(resp.Result) > 0 {
		_, _ = out.Write(resp.Result)
	}
	return nil
}

func Del(args *skel.CmdArgs, opt Options) error {

	if err := checkReadiness(opt); err != nil {
		fmt.Fprintln(os.Stderr, "daemon not ready for DEL:", err)
		return nil
	}

	pod_name := get_pod_name_regex(args.Args)
	pod_namespace := get_pod_namespace_regex(args.Args)
	pod_uid := get_pod_uid_regex(args.Args)

	req := wire.Request{
		Cmd:            wire.CmdDEL,
		ContainerID:    args.ContainerID,
		NetNS:          args.Netns,
		IfName:         args.IfName,
		PodName:        pod_name,
		PodNamespace:   pod_namespace,
		PodUID:         pod_uid,
		TimeoutSeconds: int(opt.Timeout.Seconds()),
	}
	req.IdemKey = computeIdemKey(req.Cmd, req.ContainerID, req.NetNS, req.IfName, req.PodNamespace, req.PodName, req.PodUID)
	var resp wire.Response
	logCNIRequest(&req)
	if err := uds.NewClientJSON().Call(opt.SocketPath, opt.Timeout, &req, &resp); err != nil {
		log.Printf("[CNI][%s] transport error: %v", req.Cmd, err)
		fmt.Fprintln(os.Stderr, "daemon DEL error:", err)
		return nil
	}
	logCNIResponse(&req, &resp)
	if !resp.OK {
		fmt.Fprintln(os.Stderr, "daemon DEL not OK:", nonEmpty(resp.Message, "error"))
	}

	return nil
}

func GC(args *skel.CmdArgs, out io.Writer, opt Options) error {
	if err := checkReadiness(opt); err != nil {
		return fmt.Errorf("daemon not ready: %w", err)
	}
	req := wire.Request{
		Cmd:            wire.CmdGC,
		TimeoutSeconds: int(opt.Timeout.Seconds()),
	}
	var resp wire.Response
	logCNIRequest(&req)
	if err := uds.NewClientJSON().Call(opt.SocketPath, opt.Timeout, &req, &resp); err != nil {
		log.Printf("[CNI][%s] transport error: %v", req.Cmd, err)
		return err
	}
	logCNIResponse(&req, &resp)
	if !resp.OK {
		return errors.New(nonEmpty(resp.Message, "daemon error"))
	}
	if len(resp.Result) > 0 {
		_, _ = out.Write(resp.Result)
	}

	return nil
}

func Status(args *skel.CmdArgs, out io.Writer, opt Options) error {
	if err := checkReadiness(opt); err != nil {
		return fmt.Errorf("daemon not ready: %w", err)
	}
	req := wire.Request{
		Cmd:            wire.CmdSTATUS,
		TimeoutSeconds: int(opt.Timeout.Seconds()),
	}
	var resp wire.Response
	logCNIRequest(&req)
	if err := uds.NewClientJSON().Call(opt.SocketPath, opt.Timeout, &req, &resp); err != nil {
		log.Printf("[CNI][%s] transport error: %v", req.Cmd, err)
		return err
	}
	logCNIResponse(&req, &resp)
	if !resp.OK {
		return errors.New(nonEmpty(resp.Message, "daemon not OK"))
	}
	if len(resp.Result) > 0 {
		_, _ = out.Write(resp.Result)
	}
	return nil
}

func logCNIRequest(req *wire.Request) {
	pod := fmt.Sprintf("%s/%s", emptyDash(req.PodNamespace), emptyDash(req.PodName))
	log.Printf("[CNI][%s] request -> pod=%s container=%s if=%s netns=%s", req.Cmd, pod, emptyDash(req.ContainerID), emptyDash(req.IfName), emptyDash(req.NetNS))
}

func logCNIResponse(req *wire.Request, resp *wire.Response) {
	pod := fmt.Sprintf("%s/%s", emptyDash(req.PodNamespace), emptyDash(req.PodName))
	log.Printf("[CNI][%s] response <- ok=%t msg=%s pod=%s result=%s", req.Cmd, resp.OK, resp.Message, pod, truncateResult(resp.Result))
}

func truncateResult(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	const max = 160
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}

func emptyDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}
