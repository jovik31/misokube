package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/klog/v2"
)

func signalContext() (context.Context, context.CancelFunc) {

	return signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
}

func main() {

	klog.InitFlags(nil)
	cfg := parseConfig()

	ctx, stop := signalContext()
	defer stop()

	logger := klog.FromContext(ctx).WithName("orchestrator")

	if err := run(ctx, cfg, logger); err != nil {
		logger.Error(err, "failed to run orchestrator")
		klog.Flush()
		os.Exit(1)
	}

	klog.Flush()
}
