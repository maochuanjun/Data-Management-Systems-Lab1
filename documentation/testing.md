# 测试用例与执行记录（第一阶段）

依据《大作业一》“至少六类测试”的要求，分别列出**步骤、输入、预期输出和结果**。自动化用例主要位于 tests/unit、tests/integration；界面测试位于 tests/frontend、tests/e2e。

**执行结果说明：** 2026-10-17，提交者已在 `coursework/remove-python-launchers-20261017` 分支运行 `make test`、`make integration` 和 `make verify`，并提供完整终端输出。Go 单元测试、31 项 JavaScript 前端测试及 MongoDB 集成测试均通过，`go vet` 和构建通过。以下六类用例具有对应自动化测试代码；测试包整体通过，但终端未逐一打印全部测试步骤的输入/响应详情。可选 Chromium E2E 尚未执行。

## 一、六类课程必需测试

| 编号与功能 | 步骤 | 代表输入 | 预期输出 | 本次结果 | 对应代码 |
| --- | --- | --- | --- | --- | --- |
| T01 创建问卷 | A 注册并登录 → POST /api/surveys → GET /api/surveys；B 登录后尝试访问 A 的问卷 | title=课程反馈、questions=[] | 创建 201、状态 draft、列表出现；B 不能读取 A 的私有问卷 | 对应自动化测试通过（测试包整体通过） | tests/integration/http_test.go |
| T02 添加题目 | A 在草稿中添加题目 → PUT 保存 → GET 读取；用旧 revision 再次保存 | 单选、多选、fill/text、fill/number；当前 revision | 合法题目持久化；保存 200；旧 revision 返回 409 | 对应自动化测试通过（测试包整体通过） | tests/unit/domain_test.go；tests/integration/http_test.go |
| T03 条件跳转 | 设置规则并发布 → POST /next；分别给出命中、不命中的答案；尝试提交跳过题答案 | 单选 eq、多选 contains_all、数字填空 gte；target=后续题/END | 返回正确 nextQuestionId；未命中进入下一题；伪造路径被 400 拒绝 | 对应自动化测试通过（测试包整体通过） | tests/unit/domain_test.go；tests/integration/http_test.go |
| T04 答案校验 | 发布带约束的问卷 → 用非法值调用 /next 或 /responses → 改为合法值 | 必答题空值、多选少于下限、文本过长、年龄 121；合法年龄 21 | 非法答案 400；符合题型及范围时继续或提交成功 | 对应自动化测试通过（测试包整体通过） | tests/unit/domain_test.go；tests/integration/http_test.go |
| T05 提交问卷 | B 登录 → POST /responses → 相同账号再次提交 → 检查 MongoDB 记录 | allowAnonymous=true、allowRepeat=false、anonymous=true、合法 answers | 首次 201；同账号再次提交 409；只保存一份答卷，匿名答卷不含 userId | 对应自动化测试通过（测试包整体通过） | tests/integration/http_test.go |
| T06 查询统计 | 多个账号提交已知答案 → A 查询整卷统计和指定题目统计 → 翻页查看填空 | 单选与多选不同选项、若干数字及文本答案 | totalResponses、answered、选项 count、数字 average 与原始 values 正确；分页范围正确 | 对应自动化测试通过（测试包整体通过） | tests/integration/mongo_test.go；tests/integration/http_test.go |

### 建议保留的补充边界测试

权限隔离、同账号并发去重、不同账号并发提交、截止时间、规则非法目标、旧版 type=text/number 数据兼容。这些边界直接服务于后端正确性和第二阶段的旧功能保留要求。

UI 相关辅助功能及环境启动脚本的测试不作为课程核心验收内容；不需要在项目完成报告中逐项展示开发过程。

## 二、执行方式

项目的连接参数集中在根目录 Makefile。默认使用 `compose.yaml` 的 MongoDB（本机 27018 端口），执行 `make integration` 或 `make e2e` 时会自动启动并等待容器健康，不必预先手工启动数据库。

~~~sh
make test          # Go 单元测试 + JavaScript 前端测试，不需要 MongoDB
make integration   # 自动启动默认 MongoDB，运行真实数据库集成测试
make verify        # 核心测试、静态检查及构建；不包含可选 E2E
~~~

浏览器端到端测试是可选补充，第一次运行前需安装 Node.js 开发依赖和 Chromium：

~~~sh
npm install
npx playwright install chromium
make e2e
~~~

如使用已有 MongoDB，可覆盖连接地址，避免自动启动 Docker：

~~~sh
INTEGRATION_MONGO_URI='mongodb://127.0.0.1:27017' make integration
~~~

**环境要求：** Go 1.26+、Make；默认数据库模式还需 Docker Compose；`make test` 的前端部分需 Node.js；`make e2e` 另外需 Playwright/Chromium。默认连接与服务参数写在 Makefile 中，不需要 Python 或额外的环境模板文件。数据库连接失败应检查 `docker compose ps`、`docker compose logs mongodb` 和自定义连接地址。

## 三、数据库连接故障与修复

2026-10-17 初次执行集成测试时，MongoDB 地址 `127.0.0.1:27018` 无服务监听，出现 `connect: connection refused`。当时 7 个集成用例无法连接数据库，产生的 34.3% 部分覆盖率无效。该错误不是已经验证的问卷逻辑缺陷。

处理方式：Makefile 在默认本地模式下自动启动并等待 MongoDB 容器健康；`tests/support/mongo.go` 增加快速连接预检；失败时清理不完整的 `coverage.out`。保留使用自定义 MongoDB URI 的方式。

## 四、2026-10-17 修复后实际验收

以下全部来自提交者在课程项目本机执行、粘贴于对话的终端日志，而不是 AI 自行运行的结果。

| 实际操作 | 终端结果 | 结论 |
| --- | --- | --- |
| `make test` — Go 单元测试 | `ok questionnaire/tests/unit` | 通过 |
| `make test` — JavaScript 前端测试 | `tests 31; pass 31; fail 0` | **31/31 通过** |
| `make integration` — MongoDB 健康检查 | `Container ...mongodb-1 Healthy` | 通过 |
| `make integration` — Go 单元与集成测试 | 两个测试包均为 `ok`；集成包用时约 17.4 秒 | 通过 |
| `make verify` — 核心测试复跑 | 前端 31/31，Go 单元及 MongoDB 集成包均为 `ok` | 通过 |
| `make verify` — `go vet ./...` | 正常执行，无错误输出 | 通过 |
| `make verify` — `go build -o bin/survey-lab ./cmd/server` | 正常结束，无报错 | 通过 |
| `go tool cover -func=coverage.out \| tail -n 1` | `total: (statements) 85.7%` | **核心后端包合并语句覆盖率 85.7%** |
| `make e2e` | 此次日志中没有执行 | **未验证** |

**覆盖率说明：** 日志中的 33.2% 和 80.4% 分别是单元测试包与集成测试包执行时显示的覆盖比例，不能相加；由同一 `coverage.out` 汇总工具给出的 85.7% 是本次应引用的最终合并语句覆盖率。该百分比说明执行路径覆盖，不等于全部需求或业务边界已被证明正确。

**六类必需测试的结果判定：** T01～T06 的相关单元与集成测试代码均在已通过的测试包中，故上表记为对应自动化测试通过。由于运行命令未使用 `go test -v`，终端不会逐项打印 T01～T06 的详细请求和实际响应；用例表保留“步骤、代表输入、预期输出”，不伪造独立运行截图或逐步输出。

## 五、仍需补充的课程展示材料

目前无需再将 MongoDB 集成测试记为“未复测”。如准备提交课程作业，建议保存这份真实终端日志，以及创建、题目编辑、条件跳转、错误输入、提交和统计页面的操作截图。若需要单独证明浏览器的完整流程，再安装 Playwright/Chromium 并运行 `make e2e`，把结果补录到本文。浏览器验收未执行时不得填“通过”。
