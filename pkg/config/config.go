package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Config struct {
	Kubernetes KubernetesConfig
	Registry   RegistryConfig
	Storage    StorageConfig
}

type KubernetesConfig struct {
	Kubeconfig  string
	Namespaces  []string
	RescueImage string
}

type RegistryConfig struct {
	URL      string
	Username string
	Password string
	Insecure bool
}

type StorageConfig struct {
	Type  string
	Local LocalStorageConfig
	S3    S3StorageConfig
}

type LocalStorageConfig struct {
	Path string
}

type S3StorageConfig struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	v := viper.New()

	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	setDefaults(v)

	bindEnvs(v)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	cfg := &Config{
		Kubernetes: KubernetesConfig{
			Kubeconfig:  v.GetString("kubeconfig"),
			Namespaces:  parseNamespaces(v.GetString("namespaces")),
			RescueImage: v.GetString("rescue_image"),
		},
		Registry: RegistryConfig{
			URL:      v.GetString("registry.url"),
			Username: v.GetString("registry.username"),
			Password: v.GetString("registry.password"),
			Insecure: v.GetBool("registry.insecure"),
		},
		Storage: StorageConfig{
			Type: v.GetString("storage.type"),
			Local: LocalStorageConfig{
				Path: v.GetString("storage.path"),
			},
			S3: S3StorageConfig{
				Endpoint:  v.GetString("s3.endpoint"),
				Bucket:    v.GetString("s3.bucket"),
				AccessKey: v.GetString("s3.access_key"),
				SecretKey: v.GetString("s3.secret_key"),
				Region:    v.GetString("s3.region"),
			},
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("kubeconfig", "")
	v.SetDefault("namespaces", "")
	v.SetDefault("rescue_image", "tanos-rescue:latest")
	v.SetDefault("registry.url", "")
	v.SetDefault("registry.username", "")
	v.SetDefault("registry.password", "")
	v.SetDefault("registry.insecure", false)
	v.SetDefault("storage.type", "local")
	v.SetDefault("storage.path", "/var/lib/tanos-backups")
	v.SetDefault("s3.endpoint", "")
	v.SetDefault("s3.bucket", "")
	v.SetDefault("s3.access_key", "")
	v.SetDefault("s3.secret_key", "")
	v.SetDefault("s3.region", "us-east-1")
}

func bindEnvs(v *viper.Viper) {
	envBindings := []struct {
		env string
		key string
	}{
		{"KUBECONFIG", "kubeconfig"},
		{"NAMESPACES", "namespaces"},
		{"RESCUE_IMAGE", "rescue_image"},
		{"REGISTRY_URL", "registry.url"},
		{"REGISTRY_USER", "registry.username"},
		{"REGISTRY_PASSWORD", "registry.password"},
		{"REGISTRY_INSECURE", "registry.insecure"},
		{"STORAGE_TYPE", "storage.type"},
		{"STORAGE_PATH", "storage.path"},
		{"S3_ENDPOINT", "s3.endpoint"},
		{"S3_BUCKET", "s3.bucket"},
		{"S3_ACCESS_KEY", "s3.access_key"},
		{"S3_SECRET_KEY", "s3.secret_key"},
		{"S3_REGION", "s3.region"},
	}

	for _, binding := range envBindings {
		_ = v.BindEnv(binding.key, binding.env)
	}
}

func parseNamespaces(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func (c *Config) validate() error {
	if len(c.Kubernetes.Namespaces) == 0 {
		return fmt.Errorf("NAMESPACES is required")
	}
	if c.Registry.URL == "" {
		return fmt.Errorf("REGISTRY_URL is required")
	}
	if c.Storage.Type != "local" && c.Storage.Type != "s3" {
		return fmt.Errorf("STORAGE_TYPE must be 'local' or 's3'")
	}
	if c.Storage.Type == "s3" {
		if c.Storage.S3.Endpoint == "" {
			return fmt.Errorf("S3_ENDPOINT is required when storage type is s3")
		}
		if c.Storage.S3.Bucket == "" {
			return fmt.Errorf("S3_BUCKET is required when storage type is s3")
		}
		if c.Storage.S3.AccessKey == "" {
			return fmt.Errorf("S3_ACCESS_KEY is required when storage type is s3")
		}
		if c.Storage.S3.SecretKey == "" {
			return fmt.Errorf("S3_SECRET_KEY is required when storage type is s3")
		}
	}
	return nil
}

func (c *Config) GetKubeconfigPath() string {
	if c.Kubernetes.Kubeconfig != "" {
		return c.Kubernetes.Kubeconfig
	}
	if home := os.Getenv("HOME"); home != "" {
		return home + "/.kube/config"
	}
	return ""
}

func (c *Config) BackupDir() string {
	return fmt.Sprintf("%s/%s", c.Storage.Local.Path, time.Now().Format("2006-01-02"))
}
