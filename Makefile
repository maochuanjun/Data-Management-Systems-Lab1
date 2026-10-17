# 默认连接 compose.yaml 提供的 MongoDB；可在命令行或环境变量中覆盖
MONGO_URI ?= mongodb://127.0.0.1:27018/?directConnection=true
MONGO_DB ?= questionnaire_lab1
INTEGRATION_MONGO_URI ?= $(MONGO_URI)
export MONGO_URI MONGO_DB INTEGRATION_MONGO_URI

.PHONY: run test integration e2e vet build verify

run:
	go run ./cmd/server

test:
	go test -count=1 ./tests/unit
	node --test tests/frontend/frontend.test.cjs

integration:
	go test -count=1 -race -coverpkg=./internal/... -coverprofile=coverage.out ./tests/unit ./tests/integration

# 浏览器验收为可选项：需先安装 Playwright 和 Chromium
e2e:
	RUN_E2E=1 go test -count=1 -race -v ./tests/e2e

vet:
	go vet ./...

build:
	mkdir -p bin
	go build -o bin/survey-lab ./cmd/server

verify: test integration e2e vet build
