# 问卷工坊

问卷工坊是一个基于 MongoDB 的在线问卷系统。注册用户创建、编辑、发布和关闭问卷；登录用户通过 `/survey/{id}` 填写；创建者查看单题或全量统计。完整需求、设计、HTTP 契约、开发报告和 AI 记录在 [documentation](documentation/)。

## 技术栈

- Go 1.26+，标准库 `net/http`
- MongoDB，官方 Go Driver `go.mongodb.org/mongo-driver/v2 v2.9.2`
- `bcrypt` 密码哈希；服务器端 Cookie 会话
- 静态 HTML/CSS/原生 JavaScript，无运行时前端依赖

## 快速运行

前提：安装 Go 1.26+ 和可访问的 MongoDB。先准备环境变量：

```sh
export MONGO_URI='mongodb://127.0.0.1:27018/?directConnection=true'
export MONGO_DB='questionnaire_lab1'
export PORT='8080'
export COOKIE_SECURE='false' # 本地 HTTP；HTTPS 部署时设为 true
go mod download
go run ./cmd/server
```

浏览器打开 `http://127.0.0.1:8080`。生产环境必须通过 HTTPS，并设置 `COOKIE_SECURE=true`；不要把真实 URI、密码或环境文件提交到 Git。

若已有 `~/docker/mongodb/.env` 中的本地 MongoDB 可用，可通过下列脚本启动服务。脚本仅读取该文件中的 root 认证信息，并只使用自己的 `questionnaire_lab1` 数据库；不会打印密码或改动既有数据：

```sh
python3 scripts/with-local-mongo.py go run ./cmd/server
```

另一种本地选择是 `compose.yaml` 中的独立 MongoDB，它仅绑定主机 `127.0.0.1:27018`；对应 URI 为：

```sh
docker compose up -d
export MONGO_URI='mongodb://127.0.0.1:27018/?directConnection=true'
```

## 验证

```sh
go test -count=1 -race ./...
go vet ./...

# MongoDB 集成测试（仅在明确提供测试实例时）
export INTEGRATION_MONGO_URI='mongodb://127.0.0.1:27018/?directConnection=true'
go test -count=1 -race ./...
```

2026-10-15 已使用已有本地 MongoDB 执行 `python scripts/with-local-mongo.py go test -count=1 -race -v ./...`，完整通过且未报告数据竞争。证据与范围见 [documentation/development-report.md](documentation/development-report.md)；后续变更须重新验证并更新 [documentation/ai-log.md](documentation/ai-log.md)。

## 功能摘要

- 注册、登录、登出与服务端会话
- 我的问卷、草稿乐观锁、发布后冻结、关闭和截止时间检查
- 单选、多选、文本、数字题及其校验
- 规则驱动且只向前的条件跳转
- 登录后填写、可选匿名提交、数据库唯一约束防重复
- MongoDB 聚合统计：选择计数、文本内容、数字平均值

## 文档索引

- [需求与验收](documentation/requirements.md)
- [系统和 MongoDB 设计](documentation/design.md)
- [HTTP API](documentation/api.md)
- [测试记录](documentation/testing.md)
- [开发报告](documentation/development-report.md)
- [AI 开发记录](documentation/ai-log.md)

- [Go 代码逐项讲解](documentation/go-walkthrough.md)
