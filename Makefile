# 本地默认使用 compose.yaml 的 MongoDB；外部数据库可通过 make 参数或环境变量覆盖。
DEFAULT_MONGO_URI := mongodb://127.0.0.1:27018/?directConnection=true
MONGO_URI ?= $(DEFAULT_MONGO_URI)
MONGO_DB ?= questionnaire_lab1
LISTEN_ADDR ?= 127.0.0.1
PORT ?= 8080
COOKIE_SECURE ?= false
INTEGRATION_MONGO_URI ?= $(MONGO_URI)
export MONGO_URI MONGO_DB LISTEN_ADDR PORT COOKIE_SECURE INTEGRATION_MONGO_URI

.PHONY: mongo-up run test integration e2e vet build verify

# 仅使用默认的本地连接地址时自动启动容器；自定义 URI 时不依赖 Docker。
mongo-up:
	@command -v docker >/dev/null 2>&1 || { echo "需要 Docker Compose 启动默认 MongoDB。请安装 Docker，或指定已有数据库：MONGO_URI=... make run / INTEGRATION_MONGO_URI=... make integration" >&2; exit 1; }
	@docker compose up -d --wait --wait-timeout 60 mongodb || { echo "MongoDB 启动或健康检查失败。请检查 docker compose ps 和 docker compose logs mongodb。" >&2; exit 1; }

ifeq ($(MONGO_URI),$(DEFAULT_MONGO_URI))
run: mongo-up
endif
run:
	go run ./cmd/server

# 不需要 MongoDB 的 Go 业务测试与 JavaScript 前端测试。
test:
	go test -count=1 ./tests/unit
	node --test tests/frontend/frontend.test.cjs

ifeq ($(INTEGRATION_MONGO_URI),$(DEFAULT_MONGO_URI))
integration e2e: mongo-up
endif

# 测试使用独立随机数据库；失败时不保留不完整的覆盖率文件。
integration:
	@rm -f coverage.out
	@go test -count=1 -race -coverpkg=./internal/... -coverprofile=coverage.out ./tests/unit ./tests/integration || { rm -f coverage.out; exit 1; }

# 可选浏览器验收，需先 npm install && npx playwright install chromium。
e2e:
	RUN_E2E=1 go test -count=1 -race -v ./tests/e2e

vet:
	go vet ./...

build:
	mkdir -p bin
	go build -o bin/survey-lab ./cmd/server

# 核心检查不强制安装可选的浏览器测试依赖。
verify: test integration vet build
