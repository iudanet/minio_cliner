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

// MinioClientInterface обновляем интерфейс
type MinioClientInterface interface {
	ListBuckets(ctx context.Context) ([]minio.BucketInfo, error)
	GetBucketLifecycle(ctx context.Context, bucketName string) (*lifecycle.Configuration, error)
	SetBucketLifecycle(ctx context.Context, bucketName string, config *lifecycle.Configuration) error
	BucketExists(ctx context.Context, bucketName string) (bool, error)
	ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo
	RemoveObjects(ctx context.Context, bucketName string, objectsCh <-chan minio.ObjectInfo, opts minio.RemoveObjectsOptions) <-chan minio.RemoveObjectError
}

const (
	defaultConfigPath = "."
	appName           = "minio-cleaner"
)

var (
	version    = "dev" // задается при сборке через -ldflags
	bucketName string  // Добавим переменную для хранения имени бакета
	dryRun     bool    // Новый флаг

)

// Config хранит настройки подключения к MinIO
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
	var showHelp bool

	flag.StringVar(&configFile, "config", "", "Path to config file")
	flag.BoolVar(&showVersion, "version", false, "Show version and exit")
	flag.StringVar(&bucketName, "bucket", "", "Specific bucket name to process") // Добавим новый флаг
	flag.BoolVar(&showHelp, "help", false, "Show this help message")
	flag.BoolVar(&dryRun, "dry-run", false, "Simulate cleanup without actual deletion")

	// Настраиваем кастомный вывод помощи
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		fmt.Println("Commands:")
		fmt.Println("  list    - List all buckets")
		fmt.Println("  check   - Check lifecycle policies")
		fmt.Println("  apply   - Apply default lifecycle policies")
		fmt.Println("  clean   - Clean non-current versions of all objects") // Добавлено
		fmt.Println("\nOptions:")
		flag.PrintDefaults()
	}
	flag.Parse()
	if showHelp {
		flag.Usage()
		os.Exit(0)
	}
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
		flag.Usage()
		log.Fatal("\nError: command is required")
	}

	switch args[0] {
	case "list":
		listBuckets(minioClient)
	case "check":
		checkLifecycle(minioClient)
	case "apply":
		applyLifecycle(minioClient)
	case "clean": // Добавлен новый кейс
		cleanVersions(minioClient)
	default:
		flag.Usage()
		log.Fatalf("\nError: unknown command: %s", args[0])
	}
}

func loadConfig(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.AddConfigPath(defaultConfigPath)
	}

	// Настройка чтения переменных окружения
	v.SetEnvPrefix("MINIO")
	v.AutomaticEnv()

	// Приоритет файла конфигурации над переменными окружения
	if configPath != "" {
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	} else {
		// Пытаемся прочитать конфиг из стандартных путей
		if err := v.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return nil, fmt.Errorf("error reading config: %w", err)
			}
			log.Println("Config file not found, using environment variables")
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	return &cfg, nil
}

func newMinioClient(cfg *Config) (MinioClientInterface, error) {
	return minio.New(cfg.Minio.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.Minio.AccessKey, cfg.Minio.SecretKey, ""),
		Secure: cfg.Minio.UseSSL,
	})
}

func listBuckets(client MinioClientInterface) {
	buckets, err := client.ListBuckets(context.Background())
	if err != nil {
		log.Fatalf("Error listing buckets: %v", err)
	}

	fmt.Println("Available buckets:")
	for _, bucket := range buckets {
		fmt.Printf("- %s (created: %s)\n", bucket.Name, bucket.CreationDate.Format("2006-01-02"))
	}
}

func checkLifecycle(client MinioClientInterface) {
	if bucketName != "" {
		checkSingleBucket(client, bucketName)
		return
	}

	buckets, err := client.ListBuckets(context.Background())
	if err != nil {
		log.Fatalf("Error listing buckets: %v", err)
	}

	for _, bucket := range buckets {
		checkSingleBucket(client, bucket.Name)
	}
}

func checkSingleBucket(client MinioClientInterface, name string) {
	lc, err := client.GetBucketLifecycle(context.Background(), name)
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchLifecycleConfiguration" {
			fmt.Printf("Bucket %s: ❌ No lifecycle policy\n", name)
			return
		}
		fmt.Printf("Bucket %s: ⚠️ Error checking lifecycle: %v\n", name, err)
		return
	}

	if hasCorrectPolicy(lc) {
		fmt.Printf("Bucket %s: ✅ Correct policy exists\n", name)
	} else {
		fmt.Printf("Bucket %s: ⚠️ Policy exists but not configured properly\n", name)
	}
}
func applyLifecycle(client MinioClientInterface) {
	if bucketName != "" {
		if !bucketExists(client, bucketName) {
			log.Fatalf("Bucket %s does not exist", bucketName)
		}
		processBucket(client, bucketName)
		return
	}

	buckets, err := client.ListBuckets(context.Background())
	if err != nil {
		log.Fatalf("Error listing buckets: %v", err)
	}

	for _, bucket := range buckets {
		processBucket(client, bucket.Name)
	}
}

// Вспомогательная функция для проверки существования бакета
func bucketExists(client MinioClientInterface, name string) bool {
	exists, err := client.BucketExists(context.Background(), name)
	if err != nil {
		log.Printf("Error checking bucket existence: %v", err)
		return false
	}
	return exists
}
func processBucket(client MinioClientInterface, bucketName string) {
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

func cleanVersions(client MinioClientInterface) {
	if bucketName != "" {
		cleanSingleBucket(client, bucketName)
		return
	}

	buckets, err := client.ListBuckets(context.Background())
	if err != nil {
		log.Fatalf("Error listing buckets: %v", err)
	}

	for _, bucket := range buckets {
		cleanSingleBucket(client, bucket.Name)
	}
}

func cleanSingleBucket(client MinioClientInterface, bucket string) {
	ctx := context.Background()
	log.Printf("Bucket %s: ⌛️ Cleaning versions starting", bucket)
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		log.Printf("Error checking bucket %s existence: %v", bucket, err)
		return
	}
	if !exists {
		log.Printf("Bucket %s does not exist", bucket)
		return
	}

	listOpts := minio.ListObjectsOptions{
		WithVersions: true,
		Recursive:    true,
	}

	// Счетчик для всех режимов
	var totalCount int

	if dryRun {
		// Второй проход только для dry-run: вывод деталей

		objectsCh := client.ListObjects(ctx, bucket, listOpts)
		for obj := range objectsCh {
			if !obj.IsLatest {
				totalCount++
				fmt.Printf("[Dry Run] Would delete: %s (Version ID: %s)\n",
					obj.Key, obj.VersionID)
			}
		}
		log.Printf("Bucket %s: Planning to delete %d non-current versions", bucket, totalCount)
		log.Printf("Bucket %s: ✅ Dry run completed", bucket)
		return
	}

	// Реальный режим: вывод общего количества перед удалением

	// Второй проход: удаление объектов
	objectsChForDelete := client.ListObjects(ctx, bucket, listOpts)
	for obj := range objectsChForDelete {
		if !obj.IsLatest {
			totalCount++
		}
	}
	log.Printf("Bucket %s: Deleting %d non-current versions", bucket, totalCount)

	removeObjectsCh := make(chan minio.ObjectInfo, 100)

	go func() {
		defer close(removeObjectsCh)
		for obj := range objectsChForDelete {
			if !obj.IsLatest {
				removeObjectsCh <- obj
			}
		}
	}()

	errorCh := client.RemoveObjects(ctx, bucket, removeObjectsCh, minio.RemoveObjectsOptions{})

	hasErrors := false
	for e := range errorCh {
		log.Printf("Failed to remove %s (version %s): %v",
			e.ObjectName, e.VersionID, e.Err)
		hasErrors = true
	}

	if hasErrors {
		log.Printf("Bucket %s: ❌ Clean completed with errors", bucket)
	} else {
		log.Printf("Bucket %s: ✅ Successfully deleted %d versions", bucket, totalCount)
	}
}
