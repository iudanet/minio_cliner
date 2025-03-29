# Minio Cleaner (Go Version)

## О проекте
Инструмент для автоматического управления жизненным циклом объектов в MinIO. Основные функции:
- Автоматическая настройка политик удаления старых версий
- Проверка текущих настроек бакетов
- Очистка неактуальных версий объектов
- Поддержка работы с отдельными бакетами или всеми сразу
- Режим dry-run для тестирования

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

cleaner:
  maxObjectsPerRun: 100  # Максимальное количество объектов для обработки за один запуск
```

Или используйте переменные окружения:
```bash
export MINIO_ENDPOINT="minio.example.com:9000"
export MINIO_ACCESS_KEY="ACCESS_KEY"
export MINIO_SECRET_KEY="SECRET_KEY"
export MINIO_USE_SSL="true"
export MINIO_CLEANER_MAXOBJECTSPERRUN="100"
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

# Очистить неактуальные версии (dry-run режим)
./minio-cleaner clean -dry-run

# Очистить неактуальные версии в конкретном бакете
./minio-cleaner -bucket my-bucket clean

# Показать версию
./minio-cleaner -version

# Показать справку
./minio-cleaner -help
```

## Примеры
```bash
# Проверка конкретного бакета
./minio-cleaner -bucket my-storage check

# Применение политик ко всем бакетам
./minio-cleaner apply

# Тестовая очистка (без реального удаления)
./minio-cleaner -bucket test-bucket clean -dry-run

# Очистка с ограничением в 50 объектов за запуск
MINIO_CLEANER_MAXOBJECTSPERRUN=50 ./minio-cleaner clean

# Использование кастомного конфига
./minio-cleaner -config /path/to/config.yml check
```

## Особенности работы
- **Режим dry-run**: при использовании флага `-dry-run` команда `clean` только показывает какие объекты были бы удалены, без реального удаления
- **Ограничение количества объектов**: можно ограничить количество обрабатываемых объектов за один запуск через конфиг или переменную окружения
- **Политики по умолчанию**: автоматически применяемая политика удаляет:
  - Неактуальные версии старше 1 дня
  - Оставляет 1 последнюю неактуальную версию
  - Удаляет маркеры удаления

## Лицензия
MIT License. Подробности в файле [LICENSE](LICENSE).
