package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Storage struct {
	client *minio.Client
	bucket string
}

func NewS3Storage(endpoint, accessKey, secretKey, region, bucket string, insecure bool) (*S3Storage, error) {
	endpointHost := endpoint
	secure := true

	if strings.HasPrefix(endpoint, "https://") {
		endpointHost = strings.TrimPrefix(endpoint, "https://")
		secure = true
	} else if strings.HasPrefix(endpoint, "http://") {
		endpointHost = strings.TrimPrefix(endpoint, "http://")
		secure = false
	}

	endpointHost = strings.Split(endpointHost, "/")[0]
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
		endpointHost = parsed.Host
	}

	fmt.Printf("S3 Config: endpoint=%s, bucket=%s, secure=%v, region=%s\n", endpointHost, bucket, secure, region)

	client, err := minio.New(endpointHost, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create s3 client: %w", err)
	}

	ctx := context.Background()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket %q (endpoint=%s): %w", bucket, endpointHost, err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region}); err != nil {
			return nil, fmt.Errorf("failed to create bucket: %w", err)
		}
	}

	return &S3Storage{client: client, bucket: bucket}, nil
}

func (s *S3Storage) Save(ctx context.Context, name string, r io.Reader) error {
	objectName := s.objectKey(name)

	_, err := s.client.PutObject(ctx, s.bucket, objectName, r, -1, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("failed to put object: %w", err)
	}

	return nil
}

func (s *S3Storage) Load(ctx context.Context, name string) (io.ReadCloser, error) {
	objectName := s.objectKey(name)

	obj, err := s.client.GetObject(ctx, s.bucket, objectName, minio.GetObjectOptions{})
	if err == nil {
		return obj, nil
	}

	fallbackName := s.objectKeyWithDots(name)
	obj, err = s.client.GetObject(ctx, s.bucket, fallbackName, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object: %w", err)
	}

	return obj, nil
}

func (s *S3Storage) Delete(ctx context.Context, name string) error {
	objectName := s.objectKey(name)

	if err := s.client.RemoveObject(ctx, s.bucket, objectName, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("failed to remove object: %w", err)
	}

	return nil
}

func (s *S3Storage) List(ctx context.Context) ([]ImageMetadata, error) {
	manifest, err := s.LoadManifest(ctx)
	if err != nil {
		return nil, err
	}
	return manifest, nil
}

func (s *S3Storage) Exists(ctx context.Context, name string) (bool, error) {
	objectName := s.objectKey(name)

	_, err := s.client.StatObject(ctx, s.bucket, objectName, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	if minio.ToErrorResponse(err).Code != "NoSuchKey" {
		return false, err
	}

	fallbackName := s.objectKeyWithDots(name)
	_, err = s.client.StatObject(ctx, s.bucket, fallbackName, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *S3Storage) GetSize(ctx context.Context, name string) (int64, error) {
	objectName := s.objectKey(name)

	stat, err := s.client.StatObject(ctx, s.bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to stat object: %w", err)
	}

	return stat.Size, nil
}

func (s *S3Storage) SaveManifest(ctx context.Context, images []ImageMetadata) error {
	data, err := json.MarshalIndent(images, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	_, err = s.client.PutObject(ctx, s.bucket, "manifest.json",
		toReader(data), int64(len(data)), minio.PutObjectOptions{
			ContentType: "application/json",
		})
	if err != nil {
		return fmt.Errorf("failed to put manifest: %w", err)
	}

	return nil
}

func (s *S3Storage) LoadManifest(ctx context.Context) ([]ImageMetadata, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, "manifest.json", minio.GetObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return []ImageMetadata{}, nil
		}
		return nil, fmt.Errorf("failed to get manifest: %w", err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		if isNotFound(err) {
			return []ImageMetadata{}, nil
		}
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var images []ImageMetadata
	if err := json.Unmarshal(data, &images); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	return images, nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	errResp := minio.ToErrorResponse(err)
	return errResp.Code == "NoSuchKey" ||
		errResp.Code == "NoSuchBucket" ||
		errResp.Code == "InvalidObjectState" ||
		strings.Contains(err.Error(), "does not exist")
}

func (s *S3Storage) objectKey(name string) string {
	key := name
	key = replaceAll(key, "/", "_")
	key = replaceAll(key, ":", "_")
	return key + ".tar"
}

func (s *S3Storage) objectKeyWithDots(name string) string {
	key := name
	key = replaceAll(key, "/", "_")
	key = replaceAll(key, ":", "_")
	key = replaceAll(key, ".", "_")
	return key + ".tar"
}

func replaceAll(s, old, new string) string {
	result := ""
	for _, c := range s {
		char := string(c)
		if char == old {
			result += new
		} else {
			result += char
		}
	}
	return result
}

func toReader(data []byte) *byteReader {
	return &byteReader{data: data}
}

type byteReader struct {
	data   []byte
	offset int
}

func (r *byteReader) Read(p []byte) (n int, err error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}
