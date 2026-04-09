package storage

import (
	"fmt"

	"tanos-saver/pkg/config"
)

func NewStorage(cfg *config.Config) (Storage, error) {
	switch cfg.Storage.Type {
	case "local":
		return NewLocalStorage(cfg.Storage.Local.Path)
	case "s3":
		return NewS3Storage(
			cfg.Storage.S3.Endpoint,
			cfg.Storage.S3.AccessKey,
			cfg.Storage.S3.SecretKey,
			cfg.Storage.S3.Region,
			cfg.Storage.S3.Bucket,
			false,
		)
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Storage.Type)
	}
}
