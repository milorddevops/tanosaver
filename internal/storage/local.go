package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type LocalStorage struct {
	basePath string
}

func NewLocalStorage(basePath string) (*LocalStorage, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}
	return &LocalStorage{basePath: basePath}, nil
}

func (s *LocalStorage) Save(ctx context.Context, name string, r io.Reader) error {
	filename := s.tarPath(name)
	dir := filepath.Dir(filename)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (s *LocalStorage) Load(ctx context.Context, name string) (io.ReadCloser, error) {
	filename := s.tarPath(name)
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	return f, nil
}

func (s *LocalStorage) Delete(ctx context.Context, name string) error {
	filename := s.tarPath(name)
	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

func (s *LocalStorage) List(ctx context.Context) ([]ImageMetadata, error) {
	manifest, err := s.LoadManifest(ctx)
	if err != nil {
		return nil, err
	}
	return manifest, nil
}

func (s *LocalStorage) Exists(ctx context.Context, name string) (bool, error) {
	filename := s.tarPath(name)
	_, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *LocalStorage) GetSize(ctx context.Context, name string) (int64, error) {
	filename := s.tarPath(name)
	stat, err := os.Stat(filename)
	if err != nil {
		return 0, fmt.Errorf("failed to stat file: %w", err)
	}
	return stat.Size(), nil
}

func (s *LocalStorage) SaveManifest(ctx context.Context, images []ImageMetadata) error {
	filename := filepath.Join(s.basePath, "manifest.json")

	data, err := json.MarshalIndent(images, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	return nil
}

func (s *LocalStorage) LoadManifest(ctx context.Context) ([]ImageMetadata, error) {
	filename := filepath.Join(s.basePath, "manifest.json")

	data, err := os.ReadFile(filename)
	if os.IsNotExist(err) {
		return []ImageMetadata{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var images []ImageMetadata
	if err := json.Unmarshal(data, &images); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	return images, nil
}

func (s *LocalStorage) tarPath(name string) string {
	safeName := strings.ReplaceAll(name, "/", "_")
	safeName = strings.ReplaceAll(safeName, ":", "_")
	return filepath.Join(s.basePath, safeName+".tar")
}
