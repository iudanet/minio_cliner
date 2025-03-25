package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupMinIOContainer(ctx context.Context) (testcontainers.Container, error) {
	req := testcontainers.ContainerRequest{
		Image:        "minio/minio:latest",
		Cmd:          []string{"server", "/data"},
		ExposedPorts: []string{"9000/tcp"},
		Env: map[string]string{
			"MINIO_ROOT_USER":     "testaccess",
			"MINIO_ROOT_PASSWORD": "testsecret",
		},
		WaitingFor: wait.ForLog("API:"),
	}
	return testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
}

func TestIntegrationConfigLoading(t *testing.T) {
	ctx := context.Background()

	// Запускаем MinIO контейнер
	minioContainer, err := setupMinIOContainer(ctx)
	assert.NoError(t, err)
	defer minioContainer.Terminate(ctx)

	// Получаем адрес контейнера
	endpoint, err := minioContainer.Endpoint(ctx, "")
	assert.NoError(t, err)

	// Сохраняем оригинальный рабочий каталог
	originalWD, _ := os.Getwd()
	defer os.Chdir(originalWD)

	// Создаем временную директорию
	tmpDir := t.TempDir()

	t.Run("Load from explicit config file", func(t *testing.T) {
		// Создаем конфиг-файл во временной директории
		configPath := filepath.Join(tmpDir, "test-config.yaml")
		configContent := fmt.Sprintf(`
minio:
  endpoint: "%s"
  accessKey: "testaccess"
  secretKey: "testsecret"
  useSSL: false
`, endpoint)
		err = os.WriteFile(configPath, []byte(configContent), 0644)
		assert.NoError(t, err)

		// Меняем рабочий каталог на временный
		os.Chdir(tmpDir)
		viper.Reset()

		cfg, err := loadConfig(configPath)
		assert.NoError(t, err)
		assert.Equal(t, endpoint, cfg.Minio.Endpoint)
		assert.Equal(t, "testaccess", cfg.Minio.AccessKey)
		assert.Equal(t, "testsecret", cfg.Minio.SecretKey)
	})

	t.Run("Load from environment variables", func(t *testing.T) {
		// Меняем рабочий каталог на временный (без конфигов)
		os.Chdir(tmpDir)
		viper.Reset()

		// Устанавливаем переменные окружения
		t.Setenv("MINIO_ENDPOINT", endpoint)
		t.Setenv("MINIO_ACCESSKEY", "envaccess")
		t.Setenv("MINIO_SECRETKEY", "envsecret")
		t.Setenv("MINIO_USESSL", "false")

		cfg, err := loadConfig("")
		assert.NoError(t, err)
		assert.Equal(t, endpoint, cfg.Minio.Endpoint)
		assert.Equal(t, "envaccess", cfg.Minio.AccessKey)
		assert.Equal(t, "envsecret", cfg.Minio.SecretKey)
	})

	t.Run("Priority: config file over environment", func(t *testing.T) {
		// Создаем конфиг-файл во временной директории
		configPath := filepath.Join(tmpDir, "priority-config.yaml")
		configContent := fmt.Sprintf(`
minio:
  endpoint: "%s"
  accessKey: "fileaccess"
  secretKey: "filesecret"
  useSSL: false
`, endpoint)
		err = os.WriteFile(configPath, []byte(configContent), 0644)
		assert.NoError(t, err)

		// Меняем рабочий каталог
		os.Chdir(tmpDir)
		viper.Reset()

		// Устанавливаем переменные окружения
		t.Setenv("MINIO_ENDPOINT", "wrong-endpoint")
		t.Setenv("MINIO_ACCESSKEY", "envaccess")
		t.Setenv("MINIO_SECRETKEY", "envsecret")

		cfg, err := loadConfig(configPath)
		assert.NoError(t, err)
		assert.Equal(t, endpoint, cfg.Minio.Endpoint, "Endpoint должен браться из файла")
		assert.Equal(t, "fileaccess", cfg.Minio.AccessKey, "AccessKey должен браться из файла")
		assert.Equal(t, "filesecret", cfg.Minio.SecretKey, "SecretKey должен браться из файла")
	})
}

func TestIntegrationLifecycleOperations(t *testing.T) {
	ctx := context.Background()

	// Запускаем MinIO контейнер
	minioContainer, err := setupMinIOContainer(ctx)
	assert.NoError(t, err)
	defer minioContainer.Terminate(ctx)

	// Получаем адрес контейнера
	endpoint, err := minioContainer.Endpoint(ctx, "")
	assert.NoError(t, err)

	// Создаем конфиг
	cfg := &Config{
		Minio: struct {
			Endpoint  string `mapstructure:"endpoint"`
			AccessKey string `mapstructure:"accessKey"`
			SecretKey string `mapstructure:"secretKey"`
			UseSSL    bool   `mapstructure:"useSSL"`
		}{
			Endpoint:  endpoint,
			AccessKey: "testaccess",
			SecretKey: "testsecret",
			UseSSL:    false,
		},
	}

	client, err := newMinioClient(cfg)
	assert.NoError(t, err)

	// Тестовый бакет
	testBucket := "test-bucket"

	t.Run("Create and test bucket lifecycle", func(t *testing.T) {
		// Создаем бакет
		mClient := client.(*minio.Client)
		err := mClient.MakeBucket(ctx, testBucket, minio.MakeBucketOptions{})
		assert.NoError(t, err)

		// Проверяем отсутствие политики
		checkSingleBucket(client, testBucket)

		// Применяем политику
		processBucket(client, testBucket)

		// Проверяем обновленную политику
		lc, err := client.GetBucketLifecycle(ctx, testBucket)
		assert.NoError(t, err)
		assert.True(t, hasCorrectPolicy(lc))
	})
}
