# Minio Cleaner (Go Version)

## О проекте
Инструмент для автоматического управления жизненным циклом объектов в MinIO. Основные функции:
- Автоматическая настройка политик удаления старых версий
- Проверка текущих настроек бакетов
- Поддержка работы с отдельными бакетами или всеми сразу

## Требования
- Go 1.18+
- MinIO Server
- Права администратора или достаточные права для управления бакетами

## Установка
1. Клонировать репозиторий:
```bash
git clone https://github.com/iudanet/minio_cliner.git
cd minio_cliner
```

2. Установить зависимости:
```bash
go mod download
```

3. Собрать бинарник:
```bash
go build -o minio-cleaner
```

## Конфигурация
Создайте `config.yml` на основе примера:
```yaml
minio:
  endpoint: "minio.example.com:9000"
  accessKey: "YOUR_ACCESS_KEY"
  secretKey: "YOUR_SECRET_KEY"
  useSSL: true
```

Или используйте переменные окружения:
```bash
export MINIO_ENDPOINT="minio.example.com:9000"
export MINIO_ACCESS_KEY="ACCESS_KEY"
export MINIO_SECRET_KEY="SECRET_KEY"
export MINIO_USE_SSL="true"
```

## Использование
Основные команды:
```bash
# Показать все бакеты
./minio-cleaner list

# Проверить политики (все бакеты)
./minio-cleaner check

# Применить политики (конкретный бакет)
./minio-cleaner -bucket my-bucket apply

# Показать версию
./minio-cleaner -version
```

## Примеры
```bash
# Проверка конкретного бакета
./minio-cleaner -bucket my-storage check

# Применение политик ко всем бакетам
./minio-cleaner apply

# Использование кастомного конфига
./minio-cleaner -config /path/to/config.yml check
```

2. Рекомендуемые улучшения:
- [ ] Реализовать dry-run режим для тестирования

## Лицензия
MIT License. Подробности в файле [LICENSE](LICENSE).
