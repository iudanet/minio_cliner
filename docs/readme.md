# Документация для проекта Minio Cleaner (Go версия)

## Назначение
Minio Cleaner - это инструмент на Go для автоматического управления жизненным циклом объектов в хранилище MinIO. Основные функции:
1. Получение списка всех доступных бакетов
2. Проверка и настройка политик хранения для каждого бакета
3. Автоматическое добавление политики удаления неактуальных версий файлов через 1 день

## Требования
- Go 1.18+
- Установленный MinIO сервер
- Доступ с правами администратора или достаточными правами для управления жизненным циклом бакетов

## Установка
1. Клонируйте репозиторий:
```bash
git clone https://github.com/your-repo/minio-cleaner-go.git
cd minio-cleaner-go
```

2. Установите зависимости:
```bash
go mod download
```

3. Соберите проект:
```bash
go build -o minio-cleaner
```

## Конфигурация
Создайте файл конфигурации `config.yaml`:
```yaml
minio:
  endpoint: "minio.example.com:9000"
  accessKey: "YOUR_ACCESS_KEY"
  secretKey: "YOUR_SECRET_KEY"
  useSSL: false
```

Или используйте переменные окружения:
```bash
export MINIO_ENDPOINT="minio.example.com:9000"
export MINIO_ACCESS_KEY="YOUR_ACCESS_KEY"
export MINIO_SECRET_KEY="YOUR_SECRET_KEY"
export MINIO_USE_SSL="false"
```

## Использование
### Основные команды
```bash
# Показать все бакеты
./minio-cleaner list

# Проверить политики жизненного цикла
./minio-cleaner check

# Применить политики (добавить если отсутствуют)
./minio-cleaner apply
```

### Пример кода
Основной файл `main.go`:
```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
)

type Config struct {
	Minio struct {
		Endpoint  string `yaml:"endpoint" env:"MINIO_ENDPOINT"`
		AccessKey string `yaml:"accessKey" env:"MINIO_ACCESS_KEY"`
		SecretKey string `yaml:"secretKey" env:"MINIO_SECRET_KEY"`
		UseSSL    bool   `yaml:"useSSL" env:"MINIO_USE_SSL"`
	} `yaml:"minio"`
}

func main() {
	// Загрузка конфигурации
	cfg := loadConfig()

	// Инициализация клиента MinIO
	minioClient, err := minio.New(cfg.Minio.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.Minio.AccessKey, cfg.Minio.SecretKey, ""),
		Secure: cfg.Minio.UseSSL,
	})
	if err != nil {
		log.Fatalf("Error creating MinIO client: %v", err)
	}

	// Получение списка бакетов
	buckets, err := minioClient.ListBuckets(context.Background())
	if err != nil {
		log.Fatalf("Error listing buckets: %v", err)
	}

	// Обработка каждого бакета
	for _, bucket := range buckets {
		processBucket(minioClient, bucket.Name)
	}
}

func processBucket(client MinioClientInterface, bucketName string) {
	ctx := context.Background()

	// Получение текущей политики жизненного цикла
	lc, err := client.GetBucketLifecycle(ctx, bucketName)
	if err != nil {
		if minio.ToErrorResponse(err).Code != "NoSuchLifecycleConfiguration" {
			log.Printf("Error getting lifecycle for bucket %s: %v", bucketName, err)
			return
		}
		// Политика не существует, создаем новую
		lc = createDefaultLifecycle()
		if err := client.SetBucketLifecycle(ctx, bucketName, lc); err != nil {
			log.Printf("Error setting lifecycle for bucket %s: %v", bucketName, err)
			return
		}
		log.Printf("Successfully added lifecycle policy to bucket %s", bucketName)
	} else {
		// Проверяем существующую политику
		if !hasCorrectPolicy(lc) {
			updateLifecycle(client, bucketName, lc)
		} else {
			log.Printf("Bucket %s already has correct lifecycle policy", bucketName)
		}
	}
}

func createDefaultLifecycle() *lifecycle.Configuration {
	return &lifecycle.Configuration{
		Rules: []lifecycle.Rule{
			{
				ID:     "delete-old-versions",
				Status: "Enabled",
				NoncurrentVersionExpiration: lifecycle.NoncurrentVersionExpiration{
					NoncurrentDays: 1,
				},
			},
		},
	}
}

func hasCorrectPolicy(lc *lifecycle.Configuration) bool {
	for _, rule := range lc.Rules {
		if rule.ID == "delete-old-versions" &&
		   rule.Status == "Enabled" &&
		   rule.NoncurrentVersionExpiration.NoncurrentDays == 1 {
			return true
		}
	}
	return false
}

func updateLifecycle(client MinioClientInterface, bucketName string, lc *lifecycle.Configuration) {
	// Удаляем старые правила с таким же ID
	var newRules []lifecycle.Rule
	for _, rule := range lc.Rules {
		if rule.ID != "delete-old-versions" {
			newRules = append(newRules, rule)
		}
	}

	// Добавляем новое правило
	newRules = append(newRules, lifecycle.Rule{
		ID:     "delete-old-versions",
		Status: "Enabled",
		NoncurrentVersionExpiration: lifecycle.NoncurrentVersionExpiration{
			NoncurrentDays: 1,
		},
	})

	lc.Rules = newRules

	// Применяем обновленную политику
	ctx := context.Background()
	if err := client.SetBucketLifecycle(ctx, bucketName, lc); err != nil {
		log.Printf("Error updating lifecycle for bucket %s: %v", bucketName, err)
		return
	}
	log.Printf("Successfully updated lifecycle policy for bucket %s", bucketName)
}
```

## Деплой
### Сборка Docker образа
```bash
docker build -t minio-cleaner .
```

### Запуск в Docker
```bash
docker run --rm -e MINIO_ENDPOINT=minio.example.com:9000 \
  -e MINIO_ACCESS_KEY=YOUR_KEY \
  -e MINIO_SECRET_KEY=YOUR_SECRET \
  minio-cleaner apply
```

## Логирование
Программа использует стандартный логгер Go. Для продакшн-использования рекомендуется интегрировать с:
- Zap
- Logrus
- Sentry

## Мониторинг
Рекомендуется добавить:
1. Prometheus метрики
2. Healthcheck эндпоинты
3. Интеграцию с системами оповещения

## Лицензия
Проект распространяется под лицензией MIT.
