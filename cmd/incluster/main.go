package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := cmd().ExecuteContext(ctx); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

var (
	containerID string
)

func cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "kubectl-pwru",
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), args)
		},
	}

	cobra.OnInitialize(func() {
		viper.AutomaticEnv()
	})

	cmd.Flags().StringVar(&containerID, "container-id", "", "filter pwru for network namespace of this container. Should be in the form of kubelet reporting these ids (provider://id)")

	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	return cmd
}

func run(ctx context.Context, args []string) error {
	var nsPath string
	if containerID != "" {
		pcid := new(providerContainerID)
		if err := pcid.parse(containerID); err != nil {
			return fmt.Errorf("failed to parse containerID: %w", err)
		}
		if pcid.provider == providerContainerd {
			nsGetter, err := newContainerdNamespaceGetter()
			if err != nil {
				return fmt.Errorf("creating containerd namespace getter: %w", err)
			}
			nsPath, err = nsGetter.NetworkNamespace(ctx, pcid.id)
			if err != nil {
				return err
			}
		}
	}

	if err := execPwru(ctx, nsPath, args...); err != nil {
		return fmt.Errorf("execute pwru: %w", err)
	}
	return nil
}

func execPwru(ctx context.Context, networkNamespacePath string, args ...string) error {
	if networkNamespacePath != "" {
		args = append([]string{"--filter-netns", networkNamespacePath}, args...)
	}
	cmd := exec.CommandContext(ctx, "pwru", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func checkContainerd(ctx context.Context, client *containerd.Client) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ok, err := client.IsServing(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("containerd not serving")
	}
	return nil
}

type provider string

var providerContainerd provider = "containerd"

type providerContainerID struct {
	provider provider
	id       string
}

func (p *providerContainerID) parse(containerID string) error {
	splits := strings.Split(containerID, "://")
	if len(splits) != 2 {
		return fmt.Errorf("malformed containerID %s", containerID)
	}
	switch splits[0] {
	case string(providerContainerd):
		p.provider = providerContainerd
	default:
		return fmt.Errorf("unknown provider %s", splits[0])
	}
	p.id = splits[1]
	return nil
}
