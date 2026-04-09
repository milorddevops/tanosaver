package k8s

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RescueConfig struct {
	S3Endpoint  string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string
	S3Region    string
	S3Secure    bool
	RescueImage string
}

type ImageLocation struct {
	ImageRef  string
	NodeName  string
	PodName   string
	Namespace string
}

func (c *Client) FindImagesOnNodes(ctx context.Context, images []string) ([]ImageLocation, error) {
	var locations []ImageLocation
	imageSet := make(map[string]bool)

	for _, img := range images {
		imageSet[img] = false
	}

	pods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range pods.Items {
		if pod.Spec.NodeName == "" {
			continue
		}

		for _, container := range pod.Spec.Containers {
			normalizedImage := normalizeImage(container.Image)
			if _, exists := imageSet[normalizedImage]; exists && !imageSet[normalizedImage] {
				imageSet[normalizedImage] = true
				locations = append(locations, ImageLocation{
					ImageRef:  container.Image,
					NodeName:  pod.Spec.NodeName,
					PodName:   pod.Name,
					Namespace: pod.Namespace,
				})
			}
		}

		for _, container := range pod.Spec.InitContainers {
			normalizedImage := normalizeImage(container.Image)
			if _, exists := imageSet[normalizedImage]; exists && !imageSet[normalizedImage] {
				imageSet[normalizedImage] = true
				locations = append(locations, ImageLocation{
					ImageRef:  container.Image,
					NodeName:  pod.Spec.NodeName,
					PodName:   pod.Name,
					Namespace: pod.Namespace,
				})
			}
		}

		for _, container := range pod.Spec.EphemeralContainers {
			normalizedImage := normalizeImage(container.Image)
			if _, exists := imageSet[normalizedImage]; exists && !imageSet[normalizedImage] {
				imageSet[normalizedImage] = true
				locations = append(locations, ImageLocation{
					ImageRef:  container.Image,
					NodeName:  pod.Spec.NodeName,
					PodName:   pod.Name,
					Namespace: pod.Namespace,
				})
			}
		}
	}

	return locations, nil
}

func (c *Client) CreateRescueJob(ctx context.Context, location ImageLocation, cfg RescueConfig) (string, error) {
	jobName := generateJobName(location.ImageRef)
	namespace := "default"

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":        "tanos-rescue",
				"image-name": sanitizeLabel(location.ImageRef),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            int32Ptr(0),
			ActiveDeadlineSeconds:   int64Ptr(600),
			TTLSecondsAfterFinished: int32Ptr(60),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeName:      location.NodeName,
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "rescuer",
							Image:   cfg.RescueImage,
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								buildRescueScript(location.ImageRef, cfg),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "containerd-sock",
									MountPath: "/run/containerd",
								},
								{
									Name:      "containerd-lib",
									MountPath: "/var/lib/containerd",
								},
								{
									Name:      "output",
									MountPath: "/output",
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: boolPtr(true),
							},
							Env: []corev1.EnvVar{
								{Name: "IMAGE_REF", Value: location.ImageRef},
								{Name: "S3_ENDPOINT", Value: cfg.S3Endpoint},
								{Name: "S3_BUCKET", Value: cfg.S3Bucket},
								{Name: "S3_ACCESS_KEY", Value: cfg.S3AccessKey},
								{Name: "S3_SECRET_KEY", Value: cfg.S3SecretKey},
								{Name: "S3_REGION", Value: cfg.S3Region},
								{Name: "S3_SECURE", Value: fmt.Sprintf("%v", cfg.S3Secure)},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "containerd-sock",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/run/containerd",
								},
							},
						},
						{
							Name: "containerd-lib",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/containerd",
								},
							},
						},
						{
							Name: "output",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	_, err := c.clientset.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create rescue job: %w", err)
	}

	return jobName, nil
}

func (c *Client) WaitForJob(ctx context.Context, jobName, namespace string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		job, err := c.clientset.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get job status: %w", err)
		}

		if job.Status.Succeeded > 0 {
			return nil
		}

		if job.Status.Failed > 0 {
			pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("job-name=%s", jobName),
			})
			if err == nil && len(pods.Items) > 0 {
				logs, _ := c.GetPodLogs(ctx, pods.Items[0].Namespace, pods.Items[0].Name)
				return fmt.Errorf("job failed. Logs:\n%s", logs)
			}
			return fmt.Errorf("job failed")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}

	return fmt.Errorf("job timed out after %v", timeout)
}

func (c *Client) GetPodLogs(ctx context.Context, namespace, podName string) (string, error) {
	req := c.clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{})
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()

	data, err := io.ReadAll(stream)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *Client) DeleteJob(ctx context.Context, jobName, namespace string) error {
	propagationPolicy := metav1.DeletePropagationBackground
	return c.clientset.BatchV1().Jobs(namespace).Delete(ctx, jobName, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
}

func (c *Client) CopyFileFromJob(ctx context.Context, jobName, namespace, remotePath, localPath string) error {
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		return fmt.Errorf("failed to find pod for job: %w", err)
	}

	if len(pods.Items) == 0 {
		return fmt.Errorf("no pods found for job %s", jobName)
	}

	podName := pods.Items[0].Name

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		if home := os.Getenv("HOME"); home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}

	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig,
		"cp", fmt.Sprintf("%s/%s:%s", namespace, podName, remotePath), localPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl cp failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

func buildRescueScript(imageRef string, cfg RescueConfig) string {
	normalizedImage := normalizeImage(imageRef)
	imageKey := sanitizeForStorage(normalizedImage)

	return fmt.Sprintf(`#!/bin/sh
set -e

echo "Looking for image: %s"
echo "Exporting image to tar..."
ctr -a /run/containerd/containerd.sock -n k8s.io image export /output/image.tar "%s"

    
echo "Checking tar file..."
ls -lh /output/image.tar
    
echo "Configuring S3 connection..."
mc alias set myminio "%s" "%s" "%s"

echo "Uploading to S3..."
mc cp /output/image.tar "myminio/%s/%s.tar"

echo "Upload completed successfully!"
`, imageRef, imageRef, cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, imageKey)
}

func generateJobName(imageRef string) string {
	h := fnv.New32a()
	h.Write([]byte(imageRef))
	return fmt.Sprintf("rescue-%d-%d", h.Sum32(), time.Now().Unix())
}

func sanitizeLabel(s string) string {
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, ":", "-")
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ToLower(s)

	s = strings.TrimFunc(s, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})

	if len(s) > 63 {
		s = s[:63]
	}

	s = strings.TrimFunc(s, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})

	return s
}

func sanitizeForStorage(s string) string {
	result := ""
	for _, c := range s {
		if c == '/' || c == ':' {
			result += "_"
		} else {
			result += string(c)
		}
	}
	return result
}

func int32Ptr(i int32) *int32 { return &i }
func int64Ptr(i int64) *int64 { return &i }
func boolPtr(b bool) *bool    { return &b }
