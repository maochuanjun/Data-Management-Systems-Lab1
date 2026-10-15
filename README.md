# 问卷工坊

Go + MongoDB 在线问卷系统，支持账号、问卷编辑与发布、四种题型、条件跳转、提交校验、匿名与重复策略、整卷和单题统计。

## 启动

环境要求：Go 1.26+、MongoDB。前端由 Go 程序提供，无 npm 构建步骤。

首次运行时，从安全示例创建项目自己的本地配置，并按实际 MongoDB 修改连接信息：

```sh
cp -n .env.example .env
chmod 600 .env
go mod download
make run
```

打开 <http://127.0.0.1:8080>。`.env` 位于项目根目录、已被 Git 忽略，应仅保存本项目的本地配置，不要提交真实密码。`make run` 通过 `scripts/with-local-mongo.py` 加载该文件；脚本按项目位置查找 `.env`，不受当前工作目录影响。命令行已有环境变量优先于同名 `.env` 配置。完整流程见 [操作指南](documentation/user-guide.md)。

也可使用本项目独立的开发 MongoDB：

```sh
docker compose up -d
cp -n .env.example .env
chmod 600 .env
make run
```

包装脚本会加载 `.env` 中的全部设置，并要求存在 `MONGO_URI`；`MONGO_DB` 未设置时使用 `questionnaire_lab1`，`INTEGRATION_MONGO_URI` 未设置时继承 `MONGO_URI`。Go 程序本身只读取进程环境变量，直接执行 `go run ./cmd/server` 不会自动加载 `.env`；如需直接运行，先在 shell 中设置环境变量，或执行 `python3 scripts/with-local-mongo.py go run ./cmd/server`。HTTPS 部署时设置 `COOKIE_SECURE=true`。配置项见 [.env.example](.env.example)，`make build` 输出 `bin/survey-lab`。

## 测试

全部测试位于 [`tests/`](tests/)，业务代码目录不放测试文件。

```sh
make test                                           # Go/Python 单测 + 前端回归，无须 MongoDB
python3 scripts/with-local-mongo.py make integration # 真实 MongoDB、HTTP、容量、兼容、竞态
python3 scripts/with-local-mongo.py make e2e         # 真实 Chromium 端到端验收
python3 scripts/with-local-mongo.py make verify      # 集成 + E2E + 静态检查 + 构建
```

前端测试需要 Node.js。E2E 复用已有 Playwright 与 Chromium，由 `scripts/with-browser-tools.py` 自动发现；也可设置 `PLAYWRIGHT_MODULE` 与 `PLAYWRIGHT_EXECUTABLE_PATH` 指定安装位置。没有新增业务依赖，脚本不会自动安装测试工具。详细配置见 [测试说明](documentation/testing.md)。

包装脚本会让 `INTEGRATION_MONGO_URI` 默认继承项目的 `MONGO_URI`；绕过包装脚本直接执行 `make integration` 或 `make e2e` 时，仍需自行设置 `INTEGRATION_MONGO_URI`。每次数据库测试使用随机独立库并清理；不能把普通 `go test ./...` 中跳过数据库用例的结果当作完整验收。

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
