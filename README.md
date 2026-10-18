# 在线问卷系统（MongoDB + Go）

《当代数据管理系统》第一次大作业·第一阶段。

本项目实现问卷创建、发布、填写、条件跳转和结果统计。后端用 Go，数据保存在 MongoDB，网页使用原生 HTML/CSS/JavaScript。程序不依赖关系数据库或现成问卷系统。

## 功能

- 注册、登录；用户只能管理自己创建的问卷和查看对应统计。
- 创建问卷、添加题目、设置截止时间，发布后通过 `/survey/{id}` 分享，必要时关闭。
- 三类题型：单选、多选、填空（文本或数字）；支持必答、选择数量、字数、数值范围和整数校验。
- 根据单选、多选及数字填空的答案执行问卷中配置的条件跳转。
- 登录后填写，按设置匿名提交或限制同一账号重复填写；每次答卷独立存入 MongoDB。
- 创建者查询整卷或单题统计，包括选项次数、数字平均值和填空内容。

## 运行

需要 Go 1.26+、Make、MongoDB。默认用 Docker Compose 运行本地 MongoDB：

~~~sh
make run
~~~

浏览器打开 <http://127.0.0.1:8080>。Makefile 会先启动本地 MongoDB 并等待其健康，再运行 Go 服务；默认数据库为 `questionnaire_lab1`，MongoDB 在主机 `127.0.0.1:27018`。

使用已有数据库时可覆盖连接地址：

~~~sh
MONGO_URI='mongodb://127.0.0.1:27017' MONGO_DB=my_survey make run
~~~

填写者必须先登录。匿名提交表示答卷不直接保存用户 ID，不表示免登录。

## 测试

~~~sh
make test          # Go 单元测试与 JavaScript 前端测试
make integration   # MongoDB 集成测试
make verify        # 以上核心测试、go vet、构建
~~~

可选浏览器端到端测试需要 Node.js、Playwright 和 Chromium：

~~~sh
npm ci
npx playwright install chromium
make e2e
~~~

本机验收记录：前端 **31/31**、Chromium **7/7**（含父测试）、MongoDB 集成测试通过，Go 合并语句覆盖率 **85.7%**。测试用例及结果见 [测试说明](documentation/testing.md)。

## 文档与目录

| 路径 | 内容 |
| --- | --- |
| `internal/domain/` | 问卷结构、答案校验、条件跳转 |
| `internal/store/` | MongoDB Schema、索引、数据读写和统计 |
| `internal/web/` | HTTP 接口、认证与网页 |
| `cmd/server/` | 程序入口 |
| `tests/` | 单元、集成、前端和浏览器测试 |
| [MongoDB 设计](documentation/design.md) | 集合划分、设计理由、索引与业务逻辑 |
| [API 说明](documentation/api.md) | 接口、参数与返回值 |
| [测试用例与结果](documentation/testing.md) | 六类必需测试的步骤、输入、输出和结果 |
| [AI 使用日志](documentation/ai-log.md) | 工具、提示词、代码产物及修改记录 |
