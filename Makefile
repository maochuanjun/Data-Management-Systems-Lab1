.PHONY: run test integration e2e vet build verify

# 本地启动：从项目 .env 读取 MongoDB 连接配置
run:
	python3 scripts/with-local-mongo.py go run ./cmd/server

# 快速检查：题目与跳转业务逻辑、前端操作
test:
	go test -count=1 ./tests/unit
	node --test tests/frontend/frontend.test.cjs

# 真正访问 MongoDB 的集成测试；用不同测试库隔离数据
integration:
	@test -n "$$INTEGRATION_MONGO_URI" || (echo '请设置 INTEGRATION_MONGO_URI'; exit 1)
	go test -count=1 -race -coverpkg=./internal/... -coverprofile=coverage.out ./tests/unit ./tests/integration

# 可选：使用真实浏览器完成填写流程验收
e2e:
	@test -n "$$INTEGRATION_MONGO_URI" || (echo '请设置 INTEGRATION_MONGO_URI'; exit 1)
	RUN_E2E=1 python3 scripts/with-browser-tools.py go test -count=1 -race -v ./tests/e2e

vet:
	go vet ./...

build:
	mkdir -p bin
	go build -o bin/survey-lab ./cmd/server

verify: test integration e2e vet build
