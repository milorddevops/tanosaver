package restore

import (
	"context"
	"fmt"
	"time"

	"tanos-saver/internal/registry"
	"tanos-saver/internal/storage"
)

type Restore struct {
	regClient *registry.Client
	storage   storage.Storage
}

func NewRestore(regClient *registry.Client, storage storage.Storage) *Restore {
	return &Restore{
		regClient: regClient,
		storage:   storage,
	}
}

func (r *Restore) Run(ctx context.Context) error {
	fmt.Printf("[%s] Starting restore check\n", time.Now().Format(time.RFC3339))

	images, err := r.storage.LoadManifest(ctx)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	if len(images) == 0 {
		fmt.Println("No images in backup")
		return nil
	}

	fmt.Printf("Checking %d images in backup\n", len(images))

	var restored, failed, ok int

	for i, image := range images {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		imageRef := image.Name + ":" + image.Tag
		fmt.Printf("[%d/%d] Checking: %s\n", i+1, len(images), imageRef)

		exists, err := r.regClient.ImageExists(ctx, imageRef)
		if err != nil {
			fmt.Printf("  Warning: failed to check registry: %v\n", err)
		}

		if exists {
			fmt.Printf("  OK - exists in registry\n")
			ok++
			continue
		}

		fmt.Printf("  MISSING - restoring from backup...\n")

		reader, err := r.storage.Load(ctx, imageRef)
		if err != nil {
			fmt.Printf("  Error: failed to load from storage: %v\n", err)
			failed++
			continue
		}

		if err := r.regClient.PushFromTar(ctx, reader, imageRef); err != nil {
			fmt.Printf("  Error: failed to push to registry: %v\n", err)
			failed++
			reader.Close()
			continue
		}
		reader.Close()

		fmt.Printf("  Restored successfully\n")
		restored++
	}

	fmt.Printf("[%s] Restore completed. OK: %d, Restored: %d, Failed: %d\n",
		time.Now().Format(time.RFC3339), ok, restored, failed)

	return nil
}

func (r *Restore) Check(ctx context.Context) error {
	fmt.Printf("[%s] Checking registry status\n", time.Now().Format(time.RFC3339))

	images, err := r.storage.LoadManifest(ctx)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	if len(images) == 0 {
		fmt.Println("No images in backup")
		return nil
	}

	fmt.Printf("Checking %d images\n", len(images))

	var missing []string
	var ok int

	for i, image := range images {
		imageRef := image.Name + ":" + image.Tag
		fmt.Printf("[%d/%d] %s ... ", i+1, len(images), imageRef)

		exists, err := r.regClient.ImageExists(ctx, imageRef)
		if err != nil {
			fmt.Printf("ERROR: %v\n", err)
			continue
		}

		if exists {
			fmt.Printf("OK\n")
			ok++
		} else {
			fmt.Printf("MISSING\n")
			missing = append(missing, imageRef)
		}
	}

	fmt.Printf("\n--- Summary ---\n")
	fmt.Printf("Total: %d, OK: %d, Missing: %d\n", len(images), ok, len(missing))

	if len(missing) > 0 {
		fmt.Printf("\nMissing images:\n")
		for _, img := range missing {
			fmt.Printf("  - %s\n", img)
		}
		fmt.Printf("\nRun 'tanos-saver restore' to restore missing images\n")
	}

	return nil
}
