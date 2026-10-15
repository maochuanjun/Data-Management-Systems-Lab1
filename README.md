# 问卷工坊

Go + MongoDB 在线问卷系统，支持账号、问卷编辑与发布、四种题型、条件跳转、提交校验、匿名与重复策略、整卷和单题统计。

## 启动

环境要求：Go 1.26+、MongoDB。前端由 Go 程序提供，无 npm 构建步骤。

使用已有的 `~/docker/mongodb/.env` 配置：

```sh
go mod download
python3 scripts/with-local-mongo.py go run ./cmd/server
```

打开 <http://127.0.0.1:8080>。脚本只读取现有认证配置，默认使用独立的 `questionnaire_lab1` 数据库，不复制密码或修改原 Docker 配置；已设置 `MONGO_URI` 时优先使用该值。完整流程见 [操作指南](documentation/user-guide.md)。

也可使用本项目独立的开发 MongoDB：

```sh
docker compose up -d
export MONGO_URI='mongodb://127.0.0.1:27018/?directConnection=true'
export MONGO_DB='questionnaire_lab1'
go run ./cmd/server
```

程序从环境变量读取 `MONGO_URI`、`MONGO_DB`、`LISTEN_ADDR`、`PORT`、`COOKIE_SECURE`，不会自动加载 `.env`；配置示例见 [.env.example](.env.example)。HTTPS 部署时设置 `COOKIE_SECURE=true`。`make build` 输出 `bin/survey-lab`。

## 测试

全部测试位于 [`tests/`](tests/)，业务代码目录不放测试文件。

```sh
make test                                           # 快速单测 + 前端回归，无须 MongoDB
python3 scripts/with-local-mongo.py make integration # 真实 MongoDB、HTTP、容量、兼容、竞态
python3 scripts/with-local-mongo.py make e2e         # 真实 Chromium 端到端验收
python3 scripts/with-local-mongo.py make verify      # 集成 + E2E + 静态检查 + 构建
```

前端测试需要 Node.js。E2E 复用已有 Playwright 与 Chromium，由 `scripts/with-browser-tools.py` 自动发现；也可设置 `PLAYWRIGHT_MODULE` 与 `PLAYWRIGHT_EXECUTABLE_PATH` 指定安装位置。没有新增业务依赖，脚本不会自动安装测试工具。详细配置见 [测试说明](documentation/testing.md)。

`make integration` 与 `make e2e` 在没有 `INTEGRATION_MONGO_URI` 时直接失败。每次数据库测试使用随机独立库并清理；不能把普通 `go test ./...` 中跳过数据库用例的结果当作完整验收。

## 使用约定

- 所有填写者先登录；允许匿名表示提交结果不展示账号关联。
- 发布后冻结题目与配置，创建者可以关闭问卷；截止时间由服务器校验。
- 跳转按配置顺序匹配第一条，只能跳到后续题目或结束；提交时服务器重新推导路径。
- 不允许重复时按账号去重，匿名也受限制；允许重复时按提交份数统计。
- 文本和数字原始答案默认每页 20 条，可逐页查看全部内容；计数与均值覆盖全部回答。

## 文档

- [操作指南](documentation/user-guide.md)
- [需求与验收映射](documentation/requirements.md)
- [系统与 MongoDB 设计](documentation/design.md)
- [HTTP API](documentation/api.md)
- [测试分层及记录](documentation/testing.md)
- [第二阶段扩展约定](documentation/evolution.md)
- [Go 代码讲解](documentation/go-walkthrough.md)
- [开发报告](documentation/development-report.md)、[AI 记录](documentation/ai-log.md)、[审查与修复记录](documentation/audit-2026-10-15.md)

原始本地需求参考位于被 Git 忽略的 `docs/`，不纳入产品仓库。版本 1 数据基线位于 [`tests/fixtures/v1/`](tests/fixtures/v1/)，后续版本应持续执行兼容回归。
