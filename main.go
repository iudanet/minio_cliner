package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
	"github.com/spf13/viper"
)

const (
	defaultConfigPath = "."
	appName           = "minio-cleaner"
)

var (
	version = "dev" // задается при сборке через -ldflags
)

type Config struct {
	Minio struct {
		Endpoint  string `mapstructure:"endpoint"`
		AccessKey string `mapstructure:"accessKey"`
		SecretKey string `mapstructure:"secretKey"`
		UseSSL    bool   `mapstructure:"useSSL"`
	} `mapstructure:"minio"`
}

func main() {
	var configFile string
	var showVersion bool

	flag.StringVar(&configFile, "config", "", "Path to config file")
	flag.BoolVar(&showVersion, "version", false, "Show version and exit")
	flag.Parse()

	if showVersion {
		fmt.Printf("%s version %s\n", appName, version)
		os.Exit(0)
	}

	cfg, err := loadConfig(configFile)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	minioClient, err := newMinioClient(cfg)
	if err != nil {
		log.Fatalf("Error creating MinIO client: %v", err)
	}

	args := flag.Args()
	if len(args) == 0 {
		log.Fatal("Please specify command: list, check, apply")
	}

	switch args[0] {
	case "list":
		listBuckets(minioClient)
	case "check":
		checkLifecycle(minioClient)
	case "apply":
		applyLifecycle(minioClient)
	default:
		log.Fatalf("Unknown command: %s", args[0])
	}
}

func loadConfig(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(defaultConfigPath)

	if configPath != "" {
		v.AddConfigPath(configPath)
	}

	// Установка значений по умолчанию
	v.SetDefault("minio.useSSL", false)

	// Чтение переменных окружения
	v.AutomaticEnv()
	v.SetEnvPrefix("MINIO")

	// Чтение конфига из файла
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config: %w", err)
		}
		log.Println("Config file not found, using environment variables")
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	return &cfg, nil
}

func newMinioClient(cfg *Config) (*minio.Client, error) {
	return minio.New(cfg.Minio.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.Minio.AccessKey, cfg.Minio.SecretKey, ""),
		Secure: cfg.Minio.UseSSL,
	})
}

func listBuckets(client *minio.Client) {
	buckets, err := client.ListBuckets(context.Background())
	if err != nil {
		log.Fatalf("Error listing buckets: %v", err)
	}

	fmt.Println("Available buckets:")
	for _, bucket := range buckets {
		fmt.Printf("- %s (created: %s)\n", bucket.Name, bucket.CreationDate.Format("2006-01-02"))
	}
}

func checkLifecycle(client *minio.Client) {
	buckets, err := client.ListBuckets(context.Background())
	if err != nil {
		log.Fatalf("Error listing buckets: %v", err)
	}

	for _, bucket := range buckets {
		lc, err := client.GetBucketLifecycle(context.Background(), bucket.Name)
		if err != nil {
			if minio.ToErrorResponse(err).Code == "NoSuchLifecycleConfiguration" {
				fmt.Printf("Bucket %s: ❌ No lifecycle policy\n", bucket.Name)
				continue
			}
			fmt.Printf("Bucket %s: ⚠️ Error checking lifecycle: %v\n", bucket.Name, err)
			continue
		}

		if hasCorrectPolicy(lc) {
			fmt.Printf("Bucket %s: ✅ Correct policy exists\n", bucket.Name)
		} else {
			fmt.Printf("Bucket %s: ⚠️ Policy exists but not configured properly\n", bucket.Name)
		}
	}
}

func applyLifecycle(client *minio.Client) {
	buckets, err := client.ListBuckets(context.Background())
	if err != nil {
		log.Fatalf("Error listing buckets: %v", err)
	}

	for _, bucket := range buckets {
		processBucket(client, bucket.Name)
	}
}

func processBucket(client *minio.Client, bucketName string) {
	ctx := context.Background()

	lc, err := client.GetBucketLifecycle(ctx, bucketName)
	if err != nil {
		if minio.ToErrorResponse(err).Code != "NoSuchLifecycleConfiguration" {
			log.Printf("Bucket %s: ⚠️ Error getting lifecycle: %v", bucketName, err)
			return
		}

		// Создаем новую политику
		newLC := createDefaultLifecycle()
		if err := client.SetBucketLifecycle(ctx, bucketName, newLC); err != nil {
			log.Printf("Bucket %s: ❌ Error setting lifecycle: %v", bucketName, err)
			return
		}
		log.Printf("Bucket %s: ✅ Successfully added lifecycle policy", bucketName)
		return
	}

	if hasCorrectPolicy(lc) {
		log.Printf("Bucket %s: ✅ Policy already correct", bucketName)
		return
	}

	// Обновляем существующую политику
	updatedLC := updateLifecycleConfig(lc)
	if err := client.SetBucketLifecycle(ctx, bucketName, updatedLC); err != nil {
		log.Printf("Bucket %s: ❌ Error updating lifecycle: %v", bucketName, err)
		return
	}
	log.Printf("Bucket %s: ✅ Successfully updated lifecycle policy", bucketName)
}

func createDefaultLifecycle() *lifecycle.Configuration {
	return &lifecycle.Configuration{
		Rules: []lifecycle.Rule{
			{
				ID:     "auto-clean-versions",
				Status: "Enabled",
				NoncurrentVersionExpiration: lifecycle.NoncurrentVersionExpiration{
					NoncurrentDays: lifecycle.ExpirationDays(1),
				},
			},
		},
	}
}

func hasCorrectPolicy(lc *lifecycle.Configuration) bool {
	for _, rule := range lc.Rules {
		if rule.Status == "Enabled" &&
			rule.NoncurrentVersionExpiration.NoncurrentDays == 1 {
			return true
		}
	}
	return false
}

func updateLifecycleConfig(existing *lifecycle.Configuration) *lifecycle.Configuration {
	// Фильтруем существующие правила
	var newRules []lifecycle.Rule
	for _, rule := range existing.Rules {
		if rule.ID != "auto-clean-versions" {
			newRules = append(newRules, rule)
		}
	}

	// Добавляем новое правило
	newRules = append(newRules, lifecycle.Rule{
		ID:     "auto-clean-versions",
		Status: "Enabled",
		NoncurrentVersionExpiration: lifecycle.NoncurrentVersionExpiration{
			NoncurrentDays: lifecycle.ExpirationDays(1),
		},
	})

	return &lifecycle.Configuration{
		Rules: newRules,
	}
}
