package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/typeurl/v2"
	"github.com/opencontainers/runtime-spec/specs-go"
)

var namespaceNotFoundErr = errors.New("network namespace not found")

type namespaceGetter interface {
	// NetworkNamespace returns the path to the containers network namespace
	NetworkNamespace(ctx context.Context, containerID string) (string, error)
}

type containerdNamespaceGetter struct {
	client *containerd.Client
}

func newContainerdNamespaceGetter() (namespaceGetter, error) {
	client, err := containerd.New("/run/containerd/containerd.sock")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to containerd: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := checkContainerd(ctx, client); err != nil {
		return nil, fmt.Errorf("checking containerd: %w", err)
	}

	return &containerdNamespaceGetter{
		client: client,
	}, nil
}

// NetworkNamespace implements [namespaceGetter].
func (c *containerdNamespaceGetter) NetworkNamespace(ctx context.Context, containerID string) (string, error) {
	// Scope all operations to a specific namespace
	container, err := c.client.ContainerService().Get(c.kubernetesNamespace(ctx), containerID)
	if err != nil {
		return "", err
	}

	spec, err := typeurl.UnmarshalAny(container.Spec)
	if err != nil {
		return "", err
	}
	ociSpec, ok := spec.(*specs.Spec)
	if !ok {
		return "", fmt.Errorf("containerd spec is not oci runtime spec, got %T", spec)
	}
	if ociSpec.Linux == nil {
		return "", fmt.Errorf("no linux spec present")
	}
	for _, ns := range ociSpec.Linux.Namespaces {
		if ns.Type == specs.NetworkNamespace {
			return ns.Path, nil
		}
	}
	return "", namespaceNotFoundErr
}

var _ namespaceGetter = (*containerdNamespaceGetter)(nil)

func (c *containerdNamespaceGetter) kubernetesNamespace(ctx context.Context) context.Context {
	return namespaces.WithNamespace(ctx, "k8s.io")
}
