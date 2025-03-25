# Переменные
BINARY_NAME=myapp
GOFILES=$(wildcard *.go)
GOPATH=$(shell go env GOPATH)

# Цвета для вывода
GREEN=\033[0;32m
NC=\033[0m # No Color

.PHONY: all test coverage lint clean help

# Цель по умолчанию
all: test lint

# Запуск тестов
test:
	@echo "${GREEN}Running tests...${NC}"
	go test -v ./...

# Запуск тестов с покрытием
coverage:
	@echo "${GREEN}Running tests with coverage...${NC}"
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "${GREEN}Coverage report generated in coverage.html${NC}"

# Установка линтера golangci-lint если он не установлен
ensure-lint:
	@which golangci-lint || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin v1.64.7

# Запуск линтера
lint: ensure-lint
	@echo "${GREEN}Running linter...${NC}"
	golangci-lint run ./...

# Очистка временных файлов
clean:
	@echo "${GREEN}Cleaning...${NC}"
	rm -f coverage.out coverage.html
	rm -f $(BINARY_NAME)

# Помощь
help:
	@echo "Available commands:"
	@echo "  make test       - run tests"
	@echo "  make coverage   - run tests with coverage report"
	@echo "  make lint       - run linter"
	@echo "  make clean      - remove generated files"
	@echo "  make all        - run tests and linter"
