package k8s

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type Client struct {
	clientset *kubernetes.Clientset
}

type ImageInfo struct {
	Name      string
	Namespace string
}

func NewClient(kubeconfigPath string) (*Client, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return &Client{clientset: clientset}, nil
}

func (c *Client) GetImages(ctx context.Context, namespaces []string) ([]ImageInfo, error) {
	var images []ImageInfo
	imageSet := make(map[string]bool)

	for _, ns := range namespaces {
		nsImages, err := c.getNamespaceImages(ctx, ns)
		if err != nil {
			return nil, fmt.Errorf("failed to get images from namespace %s: %w", ns, err)
		}

		for _, img := range nsImages {
			key := img.Namespace + "/" + img.Name
			if !imageSet[key] {
				imageSet[key] = true
				images = append(images, img)
			}
		}
	}

	return images, nil
}

func (c *Client) getNamespaceImages(ctx context.Context, namespace string) ([]ImageInfo, error) {
	var images []ImageInfo
	imageSet := make(map[string]bool)

	addImage := func(image string) {
		image = normalizeImage(image)
		if image != "" && !imageSet[image] {
			imageSet[image] = true
			images = append(images, ImageInfo{
				Name:      image,
				Namespace: namespace,
			})
		}
	}

	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			addImage(container.Image)
		}
		for _, container := range pod.Spec.InitContainers {
			addImage(container.Image)
		}
		for _, container := range pod.Spec.EphemeralContainers {
			addImage(container.Image)
		}
	}

	return images, nil
}

func normalizeImage(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}

	if !strings.Contains(image, "/") {
		image = "docker.io/library/" + image
	} else if !strings.Contains(image[:strings.Index(image, "/")], ".") &&
		!strings.HasPrefix(image, "localhost/") {
		image = "docker.io/" + image
	}

	return image
}

func (c *Client) ListNamespaces(ctx context.Context) ([]string, error) {
	namespaces, err := c.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	result := make([]string, len(namespaces.Items))
	for i, ns := range namespaces.Items {
		result[i] = ns.Name
	}
	return result, nil
}
