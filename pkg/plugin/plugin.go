package plugin

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/hown3d/kubectl-pwru/pkg/logger"
	"github.com/hown3d/kubectl-pwru/pkg/version"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/utils/ptr"
)

type Config struct {
	InclusterImage      string
	PodNetworkNamespace bool
}

func (c *Config) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&c.InclusterImage, "incluster-image", "ghcr.io/hown3d/kubectl-pwru/incluster:"+version.Version, "Container image to use for the pod running pwru inside the cluster")
	flags.BoolVar(&c.PodNetworkNamespace, "pod-network-namespace", true, "Whether to filter pwru to the pods network namespace")
}

type Plugin struct {
	client    kubernetes.Interface
	namespace string
	log       *logger.Logger
	cfg       *Config
}

func New(kubeflags *genericclioptions.ConfigFlags, config *Config) (*Plugin, error) {
	restconfig, err := kubeflags.ToRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	return &Plugin{
		client:    clientset,
		log:       logger.NewLogger(),
		namespace: ptr.Deref(kubeflags.Namespace, "default"),
		cfg:       config,
	}, nil
}

func (p *Plugin) Run(ctx context.Context, sourceKubeResource, targetkubeResource string) error {
	apiResources, err := restmapper.GetAPIGroupResources(p.client.Discovery())
	if err != nil {
		return err
	}
	mapper := restmapper.NewDiscoveryRESTMapper(apiResources)
	sourceGvr, name, err := parseTypeAndName(mapper, sourceKubeResource)
	if err != nil {
		return err
	}
	var (
		sourceIP netip.Addr
		nodeName string
	)

	switch sourceGvr.GroupResource().String() {
	case "pods":
		sourceIP, err = p.getPodIP(ctx, name)
		if err != nil {
			return err
		}
		nodeName, err = p.getPodNode(ctx, name)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported resource: %s", sourceGvr.GroupResource())
	}

	var containerID string
	if p.cfg.PodNetworkNamespace {
		containerID, err = p.getPodContainerID(ctx, name)
		if err != nil {
			return err
		}
	}

	var (
		targetIPs []netip.Addr
	)
	if targetkubeResource != "" {
		targetGvr, name, err := parseTypeAndName(mapper, targetkubeResource)
		if err != nil {
			return err
		}

		switch targetGvr.GroupResource().String() {
		case "services":
			targetIPs, err = p.getServiceIPs(ctx, name)
			if err != nil {
				return fmt.Errorf("getting service target ips: %w", err)
			}
		case "pods":
			podIP, err := p.getPodIP(ctx, name)
			if err != nil {
				return fmt.Errorf("getting pod target ip: %w", err)
			}
			targetIPs = append(targetIPs, podIP)
		default:
			return fmt.Errorf("unsupported resource: %s", sourceGvr)
		}
	}

	p.log.Info("pwru %s -> %s", sourceIP, targetIPs)
	pwruPod, err := p.createPwruPod(ctx, containerID, nodeName, pwruFilter(sourceIP, targetIPs))
	if err != nil {
		return err
	}
	defer func() {
		p.log.Info("deleting pwru pod %s", pwruPod.Name)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.deletePwruPod(ctx, pwruPod.Name); err != nil {
			p.log.Error(err)
		}
	}()
	p.log.Info("waiting for pwru pod %s to be ready", pwruPod.Name)
	if err := waitForPodReady(ctx, p.client, pwruPod.Namespace, pwruPod.Name, 30*time.Second); err != nil {
		return err
	}
	// stream logs
	if err := streamPodLogs(ctx, p.client, p.namespace, pwruPod.Name, os.Stdout); err != nil {
		return err
	}

	return nil
}

func (p *Plugin) getPodNode(ctx context.Context, name string) (string, error) {
	pod, err := p.client.CoreV1().Pods(p.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return pod.Spec.NodeName, nil
}
func (p *Plugin) getPodContainerID(ctx context.Context, name string) (string, error) {
	pod, err := p.client.CoreV1().Pods(p.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return pod.Status.ContainerStatuses[0].ContainerID, nil
}

// getServiceIPs returns the services clusterIP + endpoint IPs
func (p *Plugin) getServiceIPs(ctx context.Context, name string) ([]netip.Addr, error) {
	var addrs []netip.Addr
	svc, err := p.client.CoreV1().Services(p.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	clusterIP, err := netip.ParseAddr(svc.Spec.ClusterIP)
	if err != nil {
		return nil, err
	}
	addrs = append(addrs, clusterIP)

	slices, err := p.client.DiscoveryV1().EndpointSlices(p.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"kubernetes.io/service-name": name,
		}).String(),
	})
	if err != nil {
		return nil, err
	}

	for _, slice := range slices.Items {
		for _, ep := range slice.Endpoints {
			for _, epAddr := range ep.Addresses {
				ip, err := netip.ParseAddr(epAddr)
				if err != nil {
					return nil, err
				}
				addrs = append(addrs, ip)
			}
		}
	}

	return addrs, nil
}

func (p *Plugin) getPodIP(ctx context.Context, name string) (netip.Addr, error) {
	pod, err := p.client.CoreV1().Pods(p.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return netip.Addr{}, err
	}
	return netip.ParseAddr(pod.Status.PodIP)
}

func (p *Plugin) deletePwruPod(ctx context.Context, podName string) error {
	err := p.client.CoreV1().Pods(p.namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// createPwruPod replicates `kubectl apply -f -` for the pwru Pod manifest.
// Because most PodSpec fields are immutable after creation, it deletes any
// pre-existing "pwru" Pod before creating the new one.
func (p *Plugin) createPwruPod(ctx context.Context, containerID, nodeName, pwruArgs string) (*corev1.Pod, error) {
	args := []string{pwruArgs}
	if containerID != "" {
		args = append(args, "--container-id", containerID)
	}

	pod := &corev1.Pod{
		GenerateName: "pwru-",
		Namespace:    p.namespace,
		Spec: corev1.PodSpec{
			NodeName:    nodeName,
			HostNetwork: true,
			HostPID:     true,
			Containers: []corev1.Container{
				{
					Name:            "pwru",
					Image:           p.cfg.InclusterImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Args:            args,
					TTY:             true,
					Stdin:           true,
					SecurityContext: &corev1.SecurityContext{
						Privileged: new(true),
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "sys-kernel-debug",
							MountPath: "/sys/kernel/debug",
						},
						{
							Name:      "host-run",
							MountPath: "/run",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "host-run",
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/run",
						Type: new(corev1.HostPathDirectoryOrCreate),
					},
				},
				{
					Name: "sys-kernel-debug",
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/sys/kernel/debug",
						Type: new(corev1.HostPathDirectoryOrCreate),
					},
				},
			},
		},
	}

	return p.client.CoreV1().Pods(p.namespace).Create(ctx, pod, metav1.CreateOptions{})
}

// waitForPodReady replicates `kubectl wait pod pwru --for condition=Ready --timeout=90s`.
func waitForPodReady(ctx context.Context, clientset kubernetes.Interface, namespace, podName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 1*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}

// streamPodLogs replicates `kubectl logs -f pwru` by streaming stdout/stderr to out
// until the container exits or ctx is canceled.
func streamPodLogs(ctx context.Context, clientset kubernetes.Interface, namespace, podName string, out io.Writer) error {
	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: "pwru",
		Follow:    true,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("failed to open log stream: %w", err)
	}
	defer stream.Close()

	_, err = io.Copy(out, stream)
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("error streaming logs: %w", err)
	}
	return nil
}

func pwruFilter(sourceIP netip.Addr, targetIPs []netip.Addr) string {
	filterBuilder := new(strings.Builder)
	fmt.Fprintf(filterBuilder, "host %s", sourceIP)
	if len(targetIPs) > 0 {
		fmt.Fprint(filterBuilder, " or ")
		fmt.Fprint(filterBuilder, "(")
		for i, ip := range targetIPs {
			fmt.Fprintf(filterBuilder, "host %s", ip.String())
			if i+1 != len(targetIPs) {
				fmt.Fprint(filterBuilder, " or ")
			}
		}
		fmt.Fprint(filterBuilder, ")")
	}
	return filterBuilder.String()
}

type resourceTuple struct {
	Resource string
	Name     string
}

func splitResourceTypeName(s string) (resourceTuple, bool, error) {
	if !strings.Contains(s, "/") {
		return resourceTuple{}, false, nil
	}
	seg := strings.Split(s, "/")
	if len(seg) != 2 {
		return resourceTuple{}, false, fmt.Errorf("arguments in resource/name form may not have more than one slash")
	}
	resrc, name := seg[0], seg[1]
	if len(resrc) == 0 || len(name) == 0 || len(resource.SplitResourceArgument(resrc)) != 1 {
		return resourceTuple{}, false, fmt.Errorf("arguments in resource/name form must have a single resource and name")
	}
	return resourceTuple{Resource: resrc, Name: name}, true, nil
}

func parseTypeAndName(mapper meta.RESTMapper, kubeResource string) (schema.GroupVersionResource, string, error) {
	// SplitResourceArgument splits "svc/my-service" into []string{"svc", "my-service"}
	// (or returns []string{"svc"} if no slash is present).
	tuple, ok, err := splitResourceTypeName(kubeResource)
	if err != nil {
		return schema.GroupVersionResource{}, "", err
	}
	if !ok {
		return schema.GroupVersionResource{}, "", fmt.Errorf("no resource kind in argument")
	}

	// Expands shortnames ("svc") and plural/singular forms to the full GroupVersionResource
	gvr, err := mapper.ResourceFor(schema.GroupVersionResource{Resource: tuple.Resource})
	if err != nil {
		return schema.GroupVersionResource{}, "", err
	}

	return gvr, tuple.Name, nil
}
