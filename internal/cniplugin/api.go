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

	var resp wire.Response
	if err := uds.NewClientJSON().Call(opt.SocketPath, opt.Timeout, &req, &resp); err != nil {
		return err
	}
	log.Print("ADD request:", resp.Message, string(resp.Result))
	if !resp.OK {
		log.Print("ADD request failed:", resp.Message)
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
	if err := uds.NewClientJSON().Call(opt.SocketPath, opt.Timeout, &req, &resp); err != nil {
		return err
	}
	if !resp.OK {
		log.Print("CHECK request failed:", resp.Message)
		return errors.New(nonEmpty(resp.Message, "daemon error"))
	}
	if len(resp.Result) > 0 {
		_, _ = out.Write(resp.Result)
	}
	log.Print("Check request:", resp.Message, string(resp.Result))
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
	if err := uds.NewClientJSON().Call(opt.SocketPath, opt.Timeout, &req, &resp); err != nil {
		fmt.Fprintln(os.Stderr, "daemon DEL error:", err)
		return nil
	}
	if !resp.OK {
		log.Print("DEL request failed:", resp.Message)
		fmt.Fprintln(os.Stderr, "daemon DEL not OK:", nonEmpty(resp.Message, "error"))
	}

	log.Print("DEL request:", resp.Message, string(resp.Result))
	return nil
}

func GC(args *skel.CmdArgs, out io.Writer, opt Options) error {
	if err := checkReadiness(opt); err != nil {
		return fmt.Errorf("daemon not ready: %w", err)
	}
	req := wire.Request{
		Cmd:            wire.CmdSTATUS, // reuse status for GC if server supports; else define GC
		TimeoutSeconds: int(opt.Timeout.Seconds()),
	}
	var resp wire.Response
	if err := uds.NewClientJSON().Call(opt.SocketPath, opt.Timeout, &req, &resp); err != nil {
		return err
	}
	if !resp.OK {
		log.Print("GC request failed:", resp.Message)
		return errors.New(nonEmpty(resp.Message, "daemon error"))
	}
	if len(resp.Result) > 0 {
		_, _ = out.Write(resp.Result)
	}

	log.Print("GC request:", resp.Message, string(resp.Result))
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
	if err := uds.NewClientJSON().Call(opt.SocketPath, opt.Timeout, &req, &resp); err != nil {
		log.Print("Status request error:", err)
		return err
	}
	if !resp.OK {
		log.Print("Status request failed:", resp.Message)
		return errors.New(nonEmpty(resp.Message, "daemon not OK"))
	}
	if len(resp.Result) > 0 {
		_, _ = out.Write(resp.Result)
	}
	return nil
}
