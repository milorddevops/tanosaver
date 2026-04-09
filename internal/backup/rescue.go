package backup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"tanos-saver/internal/k8s"
	"tanos-saver/internal/registry"
	"tanos-saver/internal/storage"
	"tanos-saver/pkg/config"
)

type Rescue struct {
	k8sClient *k8s.Client
	regClient *registry.Client
	storage   storage.Storage
	cfg       k8s.RescueConfig
	namespace string
}

func NewRescue(k8sClient *k8s.Client, regClient *registry.Client, storage storage.Storage, storageCfg config.S3StorageConfig, rescueImage string, namespace string) *Rescue {
	return &Rescue{
		k8sClient: k8sClient,
		regClient: regClient,
		storage:   storage,
		cfg: k8s.RescueConfig{
			S3Endpoint:  storageCfg.Endpoint,
			S3Bucket:    storageCfg.Bucket,
			S3AccessKey: storageCfg.AccessKey,
			S3SecretKey: storageCfg.SecretKey,
			S3Region:    storageCfg.Region,
			S3Secure:    true,
			RescueImage: rescueImage,
		},
		namespace: namespace,
	}
}

func (r *Rescue) Run(ctx context.Context, failedImages []string) error {
	if len(failedImages) == 0 {
		return nil
	}

	fmt.Printf("\n[%s] Starting rescue for %d failed images...\n", time.Now().Format(time.RFC3339), len(failedImages))

	locations, err := r.k8sClient.FindImagesOnNodes(ctx, failedImages)
	if err != nil {
		return fmt.Errorf("failed to find images on nodes: %w", err)
	}

	if len(locations) == 0 {
		fmt.Printf("Warning: could not find any failed images on nodes\n")
		return nil
	}

	fmt.Printf("Found %d images on nodes\n", len(locations))

	existingManifest, err := r.storage.LoadManifest(ctx)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	existingKeys := make(map[string]bool)
	for _, img := range existingManifest {
		key := img.Name + ":" + img.Tag
		existingKeys[key] = true
	}

	var newImages []storage.ImageMetadata
	successCount := 0
	failedCount := 0
	skippedCount := 0

	for i, loc := range locations {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fmt.Printf("\n[%d/%d] Rescuing: %s\n", i+1, len(locations), loc.ImageRef)
		fmt.Printf("  Node: %s (pod: %s/%s)\n", loc.NodeName, loc.Namespace, loc.PodName)

		name, tag, _, err := r.regClient.GetImageInfo(ctx, loc.ImageRef)
		if err != nil {
			fmt.Printf("  Warning: failed to parse image info: %v, using raw reference\n", err)
			name, tag = parseImageRef(loc.ImageRef)
		}

		imageKey := name + ":" + tag
		if existingKeys[imageKey] {
			fmt.Printf("  Already in manifest, skipping\n")
			skippedCount++
			continue
		}

		exists, err := r.storage.Exists(ctx, name+":"+tag)
		if err != nil {
			fmt.Printf("  Warning: failed to check storage: %v\n", err)
		}
		if exists {
			fmt.Printf("  Already in storage, skipping\n")
			skippedCount++
			continue
		}

		jobName, err := r.k8sClient.CreateRescueJob(ctx, loc, r.cfg)
		if err != nil {
			fmt.Printf("  Error: failed to create rescue job: %v\n", err)
			failedCount++
			continue
		}

		fmt.Printf("  Created job: %s\n", jobName)
		fmt.Printf("  Waiting for job to complete...\n")

		err = r.k8sClient.WaitForJob(ctx, jobName, r.namespace, 10*time.Minute)
		if err != nil {
			fmt.Printf("  Error: %v\n", err)
			failedCount++
		} else {
			fmt.Printf("  Successfully rescued image\n")

			size, err := r.storage.GetSize(ctx, name+":"+tag)
			if err != nil {
				fmt.Printf("  Warning: failed to get file size: %v\n", err)
				size = 0
			}

			metadata := storage.ImageMetadata{
				Name:      name,
				Tag:       tag,
				Digest:    "",
				Size:      size,
				SavedAt:   time.Now().UTC().Format(time.RFC3339),
				Namespace: loc.Namespace,
			}
			newImages = append(newImages, metadata)
			successCount++
		}

		fmt.Printf("  Cleaning up job...\n")
		if err := r.k8sClient.DeleteJob(ctx, jobName, r.namespace); err != nil {
			fmt.Printf("  Warning: failed to delete job: %v\n", err)
		}
	}

	if len(newImages) > 0 {
		allImages := append(existingManifest, newImages...)
		if err := r.storage.SaveManifest(ctx, allImages); err != nil {
			fmt.Printf("  Error: failed to save manifest: %v\n", err)
		} else {
			fmt.Printf("Manifest updated with %d rescued images\n", len(newImages))
		}
	}

	fmt.Printf("\n[%s] Rescue completed\n", time.Now().Format(time.RFC3339))
	fmt.Printf("  Rescued: %d\n", successCount)
	fmt.Printf("  Skipped (already exists): %d\n", skippedCount)
	fmt.Printf("  Failed: %d\n", failedCount)

	return nil
}

func parseImageRef(ref string) (name, tag string) {
	tag = "latest"

	if idx := strings.LastIndex(ref, ":"); idx > strings.LastIndex(ref, "/") {
		name = ref[:idx]
		tag = ref[idx+1:]
	} else {
		name = ref
	}

	return name, tag
}
