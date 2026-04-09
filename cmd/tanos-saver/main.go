package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"tanos-saver/internal/backup"
	"tanos-saver/internal/k8s"
	"tanos-saver/internal/registry"
	"tanos-saver/internal/restore"
	"tanos-saver/internal/storage"
	"tanos-saver/pkg/config"
)

var version = "dev"

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:     "tanos-saver",
	Short:   "Backup and restore container images from Kubernetes",
	Version: version,
}

var saveCmd = &cobra.Command{
	Use:   "save",
	Short: "Save images from Kubernetes namespaces to storage",
	RunE:  runSave,
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore missing images from storage to registry",
	RunE:  runRestore,
}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check which images are missing from registry",
	RunE:  runCheck,
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List backed up images",
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(saveCmd)
	rootCmd.AddCommand(restoreCmd)
	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(listCmd)
}

func runSave(cmd *cobra.Command, args []string) error {
	ctx, cancel := contextWithSignal()
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	k8sClient, err := k8s.NewClient(cfg.GetKubeconfigPath())
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}

	regClient, err := registry.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create registry client: %w", err)
	}

	store, err := storage.NewStorage(cfg)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}

	b := backup.NewBackup(k8sClient, regClient, store, cfg)
	return b.Run(ctx, cfg.Kubernetes.Namespaces)
}

func runRestore(cmd *cobra.Command, args []string) error {
	ctx, cancel := contextWithSignal()
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	regClient, err := registry.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create registry client: %w", err)
	}

	store, err := storage.NewStorage(cfg)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}

	r := restore.NewRestore(regClient, store)
	return r.Run(ctx)
}

func runCheck(cmd *cobra.Command, args []string) error {
	ctx, cancel := contextWithSignal()
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	regClient, err := registry.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create registry client: %w", err)
	}

	store, err := storage.NewStorage(cfg)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}

	r := restore.NewRestore(regClient, store)
	return r.Check(ctx)
}

func runList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	store, err := storage.NewStorage(cfg)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}

	images, err := store.LoadManifest(ctx)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	if len(images) == 0 {
		fmt.Println("No images in backup")
		return nil
	}

	fmt.Printf("Backed up images (%d):\n\n", len(images))
	fmt.Printf("%-60s %-20s %-10s %s\n", "IMAGE", "TAG", "SIZE", "SAVED AT")
	fmt.Println(string(make([]byte, 110)))

	for _, img := range images {
		fmt.Printf("%-60s %-20s %-10s %s\n",
			truncate(img.Name, 60),
			img.Tag,
			formatSize(img.Size),
			img.SavedAt)
	}

	return nil
}

func contextWithSignal() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nReceived signal, shutting down...")
		cancel()
	}()

	return ctx, cancel
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
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
