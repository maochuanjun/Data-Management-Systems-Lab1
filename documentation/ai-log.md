# AI 开发记录


本日志只记录本仓库实际发生的协作内容；不把未发生的测试、人工修改或结论写成事实。工具为 Codex（含资料检索与代码协作）。后续每次重要修改、测试和审查应继续追加日期、提示词、输出和实际调整。

| 记录项 | 实际提示词或要求（摘录） | AI 产物 | 使用者/主线调整与状态 |
| --- | --- | --- | --- |
| 1 | 阅读 `docs/` 下的本地需求参考并协助完成项目 | 识别为需要支持演进的 MongoDB 问卷系统，开始初始版本实现准备。 | 将本地参考的功能描述作为初始需求来源。 |
| 2 | “等一下，用go吧，我顺便学学” | 停止原先的 Flask 方向，改为检索 Go 标准库与 MongoDB Go Driver 资料。 | 技术栈改为 Go。 |
| 3 | “自行管理git，保留开发记录” | 建立 README、开发报告、测试记录和本日志的文档结构。 | 开发文档保留在 `documentation/`；本地需求参考保留在忽略的 `docs/`。 |
| 4 | “注意项目的可拓展性要强” | 采用 domain/store/web 分层、稳定字符串 ID、`schemaVersion`、草稿 `revision` 和规则数据化设计。 | 明确后续版本按增量演进，生产持久化仍使用 MongoDB。 |
| 5 | “不要把大作业这个文档放进去，这是模拟一个真实系统” | 将产品文档与本地需求参考分离，改用产品/开发术语。 | 不将 `docs/` 中的参考文件纳入产品文档。 |
| 6 | “先实现可运行的完整版本，再逐项讲解” | 将目标调整为先交付可运行版本，再给出说明。 | 采用该推进顺序。 |
| 7 | 检索 Go `net/http` 与 MongoDB Go Driver v2 的认证、索引、校验、聚合资料 | 汇总 Cookie、`crypto/rand`、bcrypt、`mongo.Connect`/`Ping`、单文档原子写入、partial unique、JSON Schema、`$group/$avg` 的官方资料。 | 将会话设计为服务器端会话，禁止重复使用 `(surveyId,dedupeKey)` unique partial 索引。 |
| 8 | “Go 1.26+, official Mongo v2.9.2, net/http, bcrypt, static JS no deps… layers domain/store/web…” | 生成领域模型和校验逻辑、MongoDB store、HTTP 服务器、静态前端、集成测试和文档。 | 采用 `schemaVersion`、`revision` 和稳定字符串 ID；发布后冻结题目；匿名响应不写 `userId`。 |
| 9 | 开发审查与真实 MongoDB 回归 | 修正早期前端把匿名理解为访客可填的偏差，改为所有填写者先登录；修正启动路由注册导致的 mux panic；将统计改为单个 `$facet` 以保持总数与分题结果一致。一次 Go 环境路径异常来自环境配置而非应用代码。 | 规则、认证和统计语义见设计文档；未记录示例账号或密码。 |
| 10 | 执行真实 MongoDB 回归与浏览器流程验证 | 运行 race 检查、领域与 HTTP 集成测试、浏览器全流程和移动视口检查。 | 2026-10-15 的实测结果已写入 `testing.md` 与 `development-report.md`。 |

## 生成或协助生成的代码范围

- `internal/domain/models.go`：用户、问卷、题目、选项、规则和回答模型。
- `internal/domain/logic.go` 与 `logic_test.go`：问卷/答案校验、顺序规则匹配、服务端作答路径推导与领域测试。
- `internal/store/mongo.go`：MongoDB 连接、JSON Schema validator、索引、会话、问卷、回答与聚合统计。
- `internal/web/server.go`、`cmd/server/main.go`：HTTP 路由、会话、认证、请求保护、启动与优雅关闭。
- `internal/web/integration_test.go`：使用真实 MongoDB 的 HTTP 集成与并发验证。
- `internal/web/static/`：无依赖的单页交互界面。
- `documentation/` 与 `README.md`：初始版本需求、设计、API、开发报告、运行说明和本记录。

## 后续验证清单

后续版本应重复执行并追加记录：

1. 逐项核对本地需求参考和现有实现，特别是“填写者必须先登录”、匿名、重复提交和所有权。
2. 后续变更后重复执行 `go test ./...`、`go vet ./...` 和 MongoDB 集成测试；把真实命令、日期和结果追加到本文件与 `development-report.md`。
3. 用两个浏览器会话进行注册、编辑冲突、发布、填写、匿名、截止、关闭、重复提交和统计的手工检查。
4. 不将真实 `MONGO_URI`、Cookie 或账号密码写入版本库；检查 `.gitignore`。

AI 输出是协作草案，不替代对实现、运行验证和记录的审查。

本轮用户提供需求、技术栈、运行环境和产品约束；代码及上述修复由 AI 完成，没有把 AI 的修正记作用户手写代码。原始参考一度被加入首个本地提交，随后按用户要求在未发布前移除；当前 Git 历史不跟踪该文件。
