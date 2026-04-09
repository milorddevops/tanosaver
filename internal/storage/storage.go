package storage

import (
	"context"
	"io"
)

type ImageMetadata struct {
	Name      string `json:"name"`
	Tag       string `json:"tag"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	SavedAt   string `json:"saved_at"`
	Namespace string `json:"namespace"`
}

type Storage interface {
	Save(ctx context.Context, name string, r io.Reader) error
	Load(ctx context.Context, name string) (io.ReadCloser, error)
	Delete(ctx context.Context, name string) error
	List(ctx context.Context) ([]ImageMetadata, error)
	Exists(ctx context.Context, name string) (bool, error)
	GetSize(ctx context.Context, name string) (int64, error)
	SaveManifest(ctx context.Context, images []ImageMetadata) error
	LoadManifest(ctx context.Context) ([]ImageMetadata, error)
}
