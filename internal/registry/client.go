package registry

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/transports/alltransports"
	"github.com/containers/image/v5/types"

	"tanos-saver/pkg/config"
)

type Client struct {
	sysCtx      *types.SystemContext
	policyCtx   *signature.PolicyContext
	registryURL string
}

func NewClient(cfg *config.Config) (*Client, error) {
	sysCtx := &types.SystemContext{
		DockerDisableV1Ping: true,
	}

	if cfg.Registry.Insecure {
		sysCtx.DockerInsecureSkipTLSVerify = types.NewOptionalBool(true)
	}

	if cfg.Registry.Username != "" && cfg.Registry.Password != "" {
		sysCtx.DockerAuthConfig = &types.DockerAuthConfig{
			Username: cfg.Registry.Username,
			Password: cfg.Registry.Password,
		}
	}

	policy := &signature.Policy{
		Default: []signature.PolicyRequirement{
			signature.NewPRInsecureAcceptAnything(),
		},
	}
	policyCtx, err := signature.NewPolicyContext(policy)
	if err != nil {
		return nil, fmt.Errorf("failed to create policy context: %w", err)
	}

	return &Client{
		sysCtx:      sysCtx,
		policyCtx:   policyCtx,
		registryURL: cfg.Registry.URL,
	}, nil
}

func (c *Client) ImageExists(ctx context.Context, imageRef string) (bool, error) {
	ref, err := c.parseReference(imageRef)
	if err != nil {
		return false, err
	}

	src, err := ref.NewImageSource(ctx, c.sysCtx)
	if err != nil {
		if strings.Contains(err.Error(), "manifest unknown") ||
			strings.Contains(err.Error(), "404") ||
			strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, fmt.Errorf("failed to check image: %w", err)
	}
	defer src.Close()

	return true, nil
}

func (c *Client) PullToTar(ctx context.Context, imageRef string, w io.Writer) error {
	srcRef, err := c.parseReference(imageRef)
	if err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp("", "tanos-*.tar")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	destRef, err := alltransports.ParseImageName("docker-archive:" + tmpPath)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to parse destination: %w", err)
	}

	opts := &copy.Options{
		SourceCtx: c.sysCtx,
	}

	_, err = copy.Image(ctx, c.policyCtx, destRef, srcRef, opts)
	tmpFile.Close()
	if err != nil {
		return fmt.Errorf("failed to copy image: %w", err)
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to open temp file: %w", err)
	}
	defer f.Close()

	_, err = io.Copy(w, f)
	if err != nil {
		return fmt.Errorf("failed to copy to writer: %w", err)
	}

	return nil
}

func (c *Client) PullToTempFile(ctx context.Context, imageRef string) (string, error) {
	srcRef, err := c.parseReference(imageRef)
	if err != nil {
		return "", err
	}

	tmpFile, err := os.CreateTemp("", "tanos-*.tar")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	destRef, err := alltransports.ParseImageName("docker-archive:" + tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to parse destination: %w", err)
	}

	opts := &copy.Options{
		SourceCtx: c.sysCtx,
	}

	_, err = copy.Image(ctx, c.policyCtx, destRef, srcRef, opts)
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to copy image: %w", err)
	}

	return tmpPath, nil
}

func (c *Client) PushFromTar(ctx context.Context, r io.Reader, imageRef string) error {
	destRef, err := c.parseReference(imageRef)
	if err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp("", "tanos-*.tar")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, r); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	srcRef, err := alltransports.ParseImageName("docker-archive:" + tmpPath)
	if err != nil {
		return fmt.Errorf("failed to parse source: %w", err)
	}

	opts := &copy.Options{
		DestinationCtx: c.sysCtx,
	}

	_, err = copy.Image(ctx, c.policyCtx, destRef, srcRef, opts)
	if err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	}

	return nil
}

func (c *Client) GetManifest(ctx context.Context, imageRef string) ([]byte, error) {
	ref, err := c.parseReference(imageRef)
	if err != nil {
		return nil, err
	}

	src, err := ref.NewImageSource(ctx, c.sysCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to create image source: %w", err)
	}
	defer src.Close()

	manifestBytes, _, err := src.GetManifest(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest: %w", err)
	}

	return manifestBytes, nil
}

func (c *Client) GetDigest(ctx context.Context, imageRef string) (string, error) {
	ref, err := c.parseReference(imageRef)
	if err != nil {
		return "", err
	}

	src, err := ref.NewImageSource(ctx, c.sysCtx)
	if err != nil {
		return "", fmt.Errorf("failed to create image source: %w", err)
	}
	defer src.Close()

	rawManifest, _, err := src.GetManifest(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get manifest: %w", err)
	}

	digest, err := manifest.Digest(rawManifest)
	if err != nil {
		return "", fmt.Errorf("failed to compute digest: %w", err)
	}

	return digest.String(), nil
}

func (c *Client) parseReference(imageRef string) (types.ImageReference, error) {
	if !strings.HasPrefix(imageRef, "//") {
		imageRef = "//" + imageRef
	}

	ref, err := docker.ParseReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse reference %s: %w", imageRef, err)
	}

	return ref, nil
}

func (c *Client) NormalizeImageName(image string) string {
	if !strings.Contains(image, "/") {
		return "docker.io/library/" + image
	}
	if !strings.Contains(image[:strings.Index(image, "/")], ".") &&
		!strings.HasPrefix(image, "localhost/") {
		return "docker.io/" + image
	}
	return image
}

func (c *Client) GetImageInfo(ctx context.Context, imageRef string) (name, tag, digest string, err error) {
	normalizedRef := c.NormalizeImageName(imageRef)

	ref, err := reference.ParseNormalizedNamed(normalizedRef)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse image name: %w", err)
	}

	name = reference.FamiliarName(ref)

	tagged := reference.TagNameOnly(ref)
	if t, ok := tagged.(reference.Tagged); ok {
		tag = t.Tag()
	} else {
		tag = "latest"
	}

	digest, err = c.GetDigest(ctx, normalizedRef)
	if err != nil {
		digest = ""
	}

	return name, tag, digest, nil
}

type ImageInfo struct {
	Name      string
	Tag       string
	Digest    string
	Size      int64
	SavedAt   string
	Namespace string
}

func NewImageInfo(name, tag, digest, namespace string, size int64) ImageInfo {
	return ImageInfo{
		Name:      name,
		Tag:       tag,
		Digest:    digest,
		Size:      size,
		SavedAt:   time.Now().UTC().Format(time.RFC3339),
		Namespace: namespace,
	}
}
