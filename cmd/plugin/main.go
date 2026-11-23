package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/hown3d/kubectl-pwru/cmd/plugin/commands"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := commands.RootCmd().ExecuteContext(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			os.Exit(1)
		}
	}
}
