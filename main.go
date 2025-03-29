package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

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
	Cleaner struct {
		MaxObjectsPerRun int `mapstructure:"maxObjectsPerRun"`
	} `mapstructure:"cleaner"`
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
		cleanVersions(minioClient, cfg)
	default:
		flag.Usage()
		log.Fatalf("\nError: unknown command: %s", args[0])
	}
}

func loadConfig(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	// Устанавливаем значения по умолчанию
	v.SetDefault("cleaner.maxObjectsPerRun", 100)

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
	const ruleID = "auto-clean-versions"

	lc, err := client.GetBucketLifecycle(ctx, bucketName)
	if err != nil {
		if minio.ToErrorResponse(err).Code != "NoSuchLifecycleConfiguration" {
			log.Printf("Bucket %s: ⚠️ Error getting lifecycle: %v", bucketName, err)
			return
		}

		// Если политики нет вообще - создаем новую
		newLC := createDefaultLifecycle()
		err := client.SetBucketLifecycle(ctx, bucketName, newLC)
		if err != nil {
			log.Printf("Bucket %s: ❌ Error setting lifecycle: %v", bucketName, err)
			return
		}
		log.Printf("Bucket %s: ✅ Successfully added lifecycle policy", bucketName)
		return
	}

	// Ищем наше правило в существующей конфигурации
	var ourRule *lifecycle.Rule
	for i, rule := range lc.Rules {
		if rule.ID == ruleID {
			ourRule = &lc.Rules[i]
			break
		}
	}

	if ourRule == nil {
		// Если правила нет - добавляем его к существующим
		lc.Rules = append(lc.Rules, createDefaultLifecycle().Rules[0])
	} else if !hasCorrectRule(ourRule) {
		// Если правило есть, но не корректное - удаляем и добавляем заново
		var removed bool
		lc, removed = removeRuleByID(lc, ruleID)
		if !removed {
			log.Printf("Bucket %s: ⚠️ Failed to remove existing rule", bucketName)
			return
		}
		lc.Rules = append(lc.Rules, createDefaultLifecycle().Rules[0])
	} else {
		// Правило уже корректное - ничего не делаем
		log.Printf("Bucket %s: ✅ Policy already correct", bucketName)
		return
	}

	// Применяем обновленную конфигурацию
	if err := client.SetBucketLifecycle(ctx, bucketName, lc); err != nil {
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
					NoncurrentDays:          lifecycle.ExpirationDays(1),
					NewerNoncurrentVersions: 1,
				},
				Expiration: lifecycle.Expiration{DeleteMarker: true},
			},
		},
	}
}

func hasCorrectPolicy(lc *lifecycle.Configuration) bool {
	for _, rule := range lc.Rules {
		if hasCorrectRule(&rule) {
			return true
		}
	}
	return false
}
func hasCorrectRule(rule *lifecycle.Rule) bool {
	if rule.Status == "Enabled" &&
		rule.NoncurrentVersionExpiration.NoncurrentDays == 1 &&
		rule.NoncurrentVersionExpiration.NewerNoncurrentVersions == 1 &&
		rule.Expiration.DeleteMarker {
		return true
	}
	return false
}

func cleanVersions(client MinioClientInterface, cfg *Config) {
	if bucketName != "" {
		cleanSingleBucket(client, bucketName, cfg)
		return
	}

	buckets, err := client.ListBuckets(context.Background())
	if err != nil {
		log.Fatalf("Error listing buckets: %v", err)
	}

	for _, bucket := range buckets {
		cleanSingleBucket(client, bucket.Name, cfg)
	}
}

func cleanSingleBucket(client MinioClientInterface, bucket string, cfg *Config) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute) // Таймаут 5 минут
	defer cancel()

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

	maxObjectsPerRun := cfg.Cleaner.MaxObjectsPerRun
	var (
		totalCount     int64 // Атомарный счетчик для общего количества
		processedCount int   // Счетчик обработанных в этом прогоне
		wg             sync.WaitGroup
	)

	objectsCh := client.ListObjects(ctx, bucket, listOpts)
	removeObjectsCh := make(chan minio.ObjectInfo, maxObjectsPerRun)

	// Горутина для чтения объектов и принятия решений
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(removeObjectsCh) // Закрываем канал после завершения

		for obj := range objectsCh {
			atomic.AddInt64(&totalCount, 1)
			if !obj.IsLatest {

				select {
				case removeObjectsCh <- obj:
					processedCount++
				case <-ctx.Done():
					return // Прерываем по таймауту
				}

				if processedCount >= maxObjectsPerRun {
					log.Printf("Bucket %s: Reached limit of %d objects for this run", bucket, maxObjectsPerRun)
					return
					// Не прерываем, чтобы продолжить подсчет общего количества
				}
			}
		}
	}()

	// Горутина для обработки ошибок удаления
	errorCh := make(chan minio.RemoveObjectError)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(errorCh)

		if !dryRun {
			for err := range client.RemoveObjects(ctx, bucket, removeObjectsCh, minio.RemoveObjectsOptions{}) {
				errorCh <- err
			}
		}
	}()

	// Горутина для сбора ошибок
	hasErrors := false
	wg.Add(1)
	go func() {
		defer wg.Done()
		for e := range errorCh {
			log.Printf("Failed to remove %s (version %s): %v",
				e.ObjectName, e.VersionID, e.Err)
			hasErrors = true
		}
	}()

	// Ожидаем завершения всех горутин
	wg.Wait()

	// Логирование результатов
	total := atomic.LoadInt64(&totalCount)
	if dryRun {
		log.Printf("Bucket %s: Planning to delete %d non-current versions (would process %d now)",
			bucket, processedCount, total)
		log.Printf("Bucket %s: ✅ Dry run completed", bucket)
	} else {
		if hasErrors {
			log.Printf("Bucket %s: ❌ Clean completed with errors (processed %d/%d)",
				bucket, total, processedCount)
		} else {
			log.Printf("Bucket %s: ✅ Successfully deleted %d versions (%d remaining)",
				bucket, processedCount, total-int64(processedCount))
		}
	}
}

// removeRuleByID удаляет правило по ID из конфигурации жизненного цикла
// Возвращает новую конфигурацию и флаг, было ли правило найдено и удалено
func removeRuleByID(lfcCfg *lifecycle.Configuration, ilmID string) (*lifecycle.Configuration, bool) {
	if lfcCfg == nil || len(lfcCfg.Rules) == 0 {
		return lfcCfg, false
	}

	n := 0
	for _, rule := range lfcCfg.Rules {
		if rule.ID != ilmID {
			lfcCfg.Rules[n] = rule
			n++
		}
	}

	if n == len(lfcCfg.Rules) {
		// Правило не найдено
		return lfcCfg, false
	}

	lfcCfg.Rules = lfcCfg.Rules[:n]
	return lfcCfg, true
}
