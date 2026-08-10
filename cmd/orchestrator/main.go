package main

import (
	"context"
	"os"
)

func signalContext() context.Context {

}

func main() {

	ctx := signalContext()
	if err := run(ctx); err != nil {
		os.Exit(1)
	}
}
