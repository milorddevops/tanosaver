package backup

import (
	"context"
	"fmt"
	"os"
	"time"

	"tanos-saver/internal/k8s"
	"tanos-saver/internal/registry"
	"tanos-saver/internal/storage"
	"tanos-saver/pkg/config"
)

type Backup struct {
	k8sClient    *k8s.Client
	regClient    *registry.Client
	storage      storage.Storage
	rescue       *Rescue
	rescueNS     string
	enableRescue bool
}

func NewBackup(k8sClient *k8s.Client, regClient *registry.Client, storage storage.Storage, cfg *config.Config) *Backup {
	enableRescue := cfg.Storage.Type == "s3"
	var rescue *Rescue
	if enableRescue {
		rescue = NewRescue(k8sClient, regClient, storage, cfg.Storage.S3, cfg.Kubernetes.RescueImage, "default")
	}
	return &Backup{
		k8sClient:    k8sClient,
		regClient:    regClient,
		storage:      storage,
		rescue:       rescue,
		rescueNS:     "default",
		enableRescue: enableRescue,
	}
}

func (b *Backup) Run(ctx context.Context, namespaces []string) error {
	fmt.Printf("[%s] Starting backup for namespaces: %v\n", time.Now().Format(time.RFC3339), namespaces)

	images, err := b.k8sClient.GetImages(ctx, namespaces)
	if err != nil {
		return fmt.Errorf("failed to get images: %w", err)
	}

	fmt.Printf("Found %d unique images\n", len(images))

	existingManifest, err := b.storage.LoadManifest(ctx)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	existingImages := make(map[string]bool)
	for _, img := range existingManifest {
		key := img.Name + ":" + img.Tag
		existingImages[key] = true
	}

	var newImages []storage.ImageMetadata
	var failedImages []string
	skippedCount := 0

	for i, image := range images {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fmt.Printf("[%d/%d] Processing: %s\n", i+1, len(images), image.Name)

		name, tag, digest, err := b.regClient.GetImageInfo(ctx, image.Name)
		if err != nil {
			fmt.Printf("  Warning: failed to get image info: %v\n", err)
			name = image.Name
			if idx := len(name) - 1; idx > 0 {
				for j, c := range name {
					if c == ':' {
						tag = name[j+1:]
						name = name[:j]
						break
					}
				}
			}
			if tag == "" {
				tag = "latest"
			}
		}

		imageKey := name + ":" + tag
		if existingImages[imageKey] {
			fmt.Printf("  Already backed up, skipping\n")
			skippedCount++
			continue
		}

		exists, err := b.storage.Exists(ctx, name+":"+tag)
		if err != nil {
			fmt.Printf("  Warning: failed to check storage: %v\n", err)
		}
		if exists {
			fmt.Printf("  Already in storage, skipping\n")
			skippedCount++
			continue
		}

		fmt.Printf("  Pulling image...\n")
		tmpPath, err := b.regClient.PullToTempFile(ctx, image.Name)
		if err != nil {
			fmt.Printf("  Error: failed to pull image: %v\n", err)
			failedImages = append(failedImages, image.Name)
			continue
		}

		stat, err := os.Stat(tmpPath)
		if err != nil {
			fmt.Printf("  Error: failed to get file size: %v\n", err)
			os.Remove(tmpPath)
			failedImages = append(failedImages, image.Name)
			continue
		}
		size := stat.Size()

		fmt.Printf("  Saving to storage (%d bytes)...\n", size)
		f, err := os.Open(tmpPath)
		if err != nil {
			fmt.Printf("  Error: failed to open temp file: %v\n", err)
			os.Remove(tmpPath)
			failedImages = append(failedImages, image.Name)
			continue
		}

		err = b.storage.Save(ctx, name+":"+tag, f)
		f.Close()
		os.Remove(tmpPath)

		if err != nil {
			fmt.Printf("  Error: failed to save: %v\n", err)
			failedImages = append(failedImages, image.Name)
			continue
		}

		metadata := storage.ImageMetadata{
			Name:      name,
			Tag:       tag,
			Digest:    digest,
			Size:      size,
			SavedAt:   time.Now().UTC().Format(time.RFC3339),
			Namespace: image.Namespace,
		}

		newImages = append(newImages, metadata)
		fmt.Printf("  Saved: %s:%s (%s)\n", name, tag, formatSize(size))
	}

	if len(newImages) > 0 {
		allImages := append(existingManifest, newImages...)
		if err := b.storage.SaveManifest(ctx, allImages); err != nil {
			return fmt.Errorf("failed to save manifest: %w", err)
		}
		fmt.Printf("Manifest updated with %d new images\n", len(newImages))
	}

	fmt.Printf("[%s] Backup completed\n", time.Now().Format(time.RFC3339))
	fmt.Printf("  Total images: %d\n", len(images))
	fmt.Printf("  Saved: %d\n", len(newImages))
	fmt.Printf("  Skipped (already exists): %d\n", skippedCount)
	fmt.Printf("  Failed: %d\n", len(failedImages))
	if len(failedImages) > 0 {
		fmt.Printf("  Failed images:\n")
		for _, img := range failedImages {
			fmt.Printf("    - %s\n", img)
		}

		if b.enableRescue {
			if err := b.rescue.Run(ctx, failedImages); err != nil {
				fmt.Printf("Rescue process error: %v\n", err)
			}
		} else {
			fmt.Printf("\nNote: Rescue from nodes is only available with S3 storage\n")
		}
	}

	return nil
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
