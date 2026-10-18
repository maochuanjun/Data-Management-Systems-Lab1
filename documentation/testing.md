# 测试用例与结果

本项目按《大作业一》要求，对创建问卷、添加题目、条件跳转、输入校验、提交问卷和统计结果分别设置测试。单元测试使用 Go `testing`，数据库集成测试使用独立的 MongoDB 测试库，前端使用 Node.js 测试，浏览器验收使用 Playwright/Chromium。

## 1. 六类必需测试

| 编号 | 测试步骤 | 输入与条件 | 预期输出 | 实际结果 | 主要代码 |
| --- | --- | --- | --- | --- | --- |
| T01 创建问卷 | owner 注册并自动登录后创建草稿，读取自己的问卷列表；alice 尝试读取该私有问卷 | 标题“集成测试”，初始题目为空 | 创建返回 201、状态为 `draft`；owner 列表含该问卷；alice 读取详情及统计均返回 404，自己的列表为空 | 通过 | `tests/integration/http_test.go` 中 `TestMongoHTTPIntegration` |
| T02 添加题目 | 向草稿加入单选、多选、文本填空和数字填空后保存；检查返回的问卷；使用原 revision 再次更新 | `single`、`multiple`、`fill/text`、`fill/number`，带当前 `revision` | 首次保存返回 200，`revision` 加 1；使用旧 revision 返回 409；发布后读取可见内容仍保留填空子类型 | 通过 | `tests/integration/http_test.go` 中 `TestMongoHTTPIntegration` |
| T03 条件跳转 | 配置规则后调用 `/next`，分别给出匹配与不匹配答案；最终提交时额外携带被跳过题目的答案 | q1：`a→q3`、`b→q2`、`end→END`；q2：`[x,y]→q4`；q3：`18→END`、`10→q4` | 合法请求返回 200 和对应 `nextQuestionId`；提交 `q1=a,q3=20,q2=null` 返回 400 | 通过 | `tests/integration/http_test.go` 中 `TestMongoHTTPIntegration`；`tests/unit/domain_test.go` |
| T04 输入校验 | 调用 `/next` 分别提交非法与合法答案；检查数字、选择数量、文本长度和必答约束 | 主 HTTP 用例 q3 为 0–100 的必答整数：101、10.5、字符串 `"18"` 非法，数值 18 合法；q2 空数组、重复选项和超量选项非法；q4 要求 2–3 个码点 | 非法答案返回 400；q3 数值 18 返回 200，`nextQuestionId=END`；`中文🙂` 可通过文本校验 | 通过 | `tests/integration/http_test.go` 中 `TestMongoHTTPIntegration`；`tests/unit/domain_test.go` 中 `TestValidateAnswer` |
| T05 提交问卷 | alice 提交匿名答卷，再以匿名或实名方式尝试重复提交；另建允许重复的问卷连续提交 | `allowAnonymous=true`、`allowRepeat=false`、本次 `anonymous=true`；另一个问卷 `allowRepeat=true` | 首次 201 并保存，匿名记录没有 `userId`；该账号再次匿名或实名提交均为 409；允许重复的问卷连续 5 次提交均为 201，保留 5 份记录 | 通过 | `tests/integration/http_test.go` 中 `TestMongoHTTPIntegration` |
| T06 统计查询 | 按下表提交三份固定答案，创建者读取整卷、q3 单题及每页 1 个数字的分页结果 | q1 依次为 a、b、b；q2 为 `[x,y]`、`[x]`；q3 为 20、10；文本为 `中文🙂`、`中文` | `totalResponses=3`；q1 为 a:1、b:2、end:0；q2 为 x:2、y:1、z:0；q3 `answered=2,average=15`；分页完整返回 20、10 | 通过 | `tests/integration/http_test.go` 中 `TestMongoHTTPIntegration`；`tests/integration/mongo_test.go` |

### T06 的固定输入与检查点

以下检查发生在主 HTTP 集成测试的三份初始答卷全部保存后、后续并发请求开始前。q1 的 a 分支跳过 q2；q2 同时选择 x、y 时跳过 q3，因此跳过题目不提供答案。

| 填写账号 | q1 单选 | q2 多选 | q3 数字 | q4 文本 |
| --- | --- | --- | --- | --- |
| alice | a | 跳过 | 20 | 跳过 |
| bob | b | x、y | 跳过 | 中文🙂 |
| charlie | b | x | 10 | 中文 |

创建者请求 `GET /api/surveys/{id}/stats`，预期整卷提交数为 3；q1、q2、q3、q4 的 `answered` 分别为 3、2、2、2。q3 的平均值为 `(20+10)/2=15`。请求 `?questionId=q3` 只返回 q3，整卷提交数仍为 3；以 `limit=1` 分别读取 `offset=0`、`offset=1`，两页各含一个数字，合并得到 20 和 10，平均值始终为 15。

`TestStatsPaginationAndSummaries` 另检验答案缺失、`null`、空字符串、空数组和数字 0 的定义：缺失及 `null` 不计 `answered`，允许的空字符串/空数组计入，数字 0 正常参与平均值。该用例的数字为 1、3、5、0，平均值为 2.25。

### 其余核心与本轮回归

- `TestMultipleChoiceExactCount` 将 `minSelect`、`maxSelect` 都设为 2：选择 1 个、3 个或重复的两个 ID 均失败，选择两个不同选项通过。
- `TestDistinctAccountsSubmitConcurrently` 检查不同账号并发提交；主 HTTP 集成测试另检查同一账号的并发请求仅保存一份答卷。
- 既有测试还覆盖关闭与截止时间、历史题型兼容、特殊题目 ID、原始答案分页和大文本统计。E2E 的年龄示例单独设置为 0–120，输入 121 被拒绝、21 可提交，与上表 0–100 的 HTTP 用例不是同一组配置。
- **答卷请求容量回归：** `tests/unit/request_size_test.go` 使用真实 HTTP 处理器和领域校验，仅替换数据库边界。`TestLargeChineseResponseCanBeSubmitted` 提交 36 题、每题 10,000 个“中”，逐题 `/next` 均预期 200，整卷请求超过原 1 MiB 限制但仍预期 201，保存的答案及 36 项路径保持完整。`TestMaximumEscapedUnicodeResponseCanBeSubmitted` 使用 100 题、每题 10,000 个非 BMP 字符，以 JSON 代理对转义编码并使用 64 字节题目 ID，约 12 MB 的请求预期 201。`TestRequestBodyLimits` 分别覆盖答卷 16 MiB 和其他 JSON 接口 1 MiB 的超限数据及尾部空白，均预期 413；`TestResponseBodyAtLimitCanBeSubmitted` 检查恰好 16 MiB 的合法请求预期 201；`TestMalformedJSONRemainsBadRequest` 检查损坏 JSON、多对象和未知字段仍为 400。
- **长答卷实际入库回归：** `tests/integration/response_size_test.go` 中 `TestMaximumEscapedUnicodeResponsePersistsInMongoDB` 通过真实 HTTP 和 MongoDB 提交 100 题、每题 10,000 个转义后的非 BMP 字符，预期 201。随后独立读回 BSON 文档，检查约 4 MB 的实际存储、100 个答案、每题完整字符、作答路径及用户关联，确认只新增一份完整答卷。
- **旧账号再次提交回归：** `tests/integration/compatibility_test.go` 中 `TestVersionOneDocumentsRemainUsable` 装载 v1 历史样例后连续执行两次 `Init()`，恢复历史答卷中原填写账号的登录会话；其第一次实名重试和匿名重试都应返回 409，原答卷继续保留。新账号仍能按旧规则填写，旧、新答卷能共同统计；移除新增辅助键后，历史答卷与原样例逐字段相同。
- **回填幂等与数据保留回归：** `tests/integration/legacy_dedupe_test.go` 中 `TestLegacyDedupeMigrationPreservesRecordsAndPolicies` 准备 12 份历史记录，覆盖同账号多份重复、已有去重键、身份可识别或不可识别的匿名记录，以及允许重复填写的问卷。删除测试库的响应索引后并发执行 3 个 `Init()`，再顺序执行 2 次；预期只给选定记录补辅助键，所有旧记录及统计不变，已有有效键优先，其他重复旧记录不删除。`TestLegacyDedupeMigrationReturnsWriteFailures` 用缺少必需字段的历史文档触发回填写入失败，预期初始化返回 MongoDB 校验错误，原记录不被修改。

## 2. 运行方法

运行核心测试需要 Go、Make 和 Node.js；其中集成测试需要 MongoDB，带 `-race` 的集成/E2E 测试需要可用的 C 编译工具链。在项目根目录执行：

~~~sh
make test          # Go 单元测试 + JavaScript 前端测试
make integration   # 真实 MongoDB 集成测试
make verify        # 以上核心测试 + go vet + Go 构建
~~~

默认数据库由 Makefile 调用 Docker Compose 启动并检查健康状态。集成测试使用随机命名的独立测试数据库，不会清空正常问卷数据。

需要完整浏览器验收时，先安装可选依赖：

~~~sh
npm ci
npx playwright install chromium
make e2e
~~~

使用其他 MongoDB 时，可以覆盖 `INTEGRATION_MONGO_URI`，例如：

~~~sh
INTEGRATION_MONGO_URI='mongodb://127.0.0.1:27017' make integration
~~~

使用默认本地实例时不必单独准备环境变量文件。

## 3. 实际执行结果（2026-10-18）

| 检查 | 本轮结果 |
| --- | --- |
| Go 单元测试 | 通过；与集成测试共同开启 `-race`，无跳过或失败 |
| JavaScript 前端测试 | **31/31 通过**，无跳过或失败 |
| 真实 MongoDB 集成测试与 race 检测 | 通过；单元与集成共 25 个顶层测试、135 条通过记录（含子测试），无失败或跳过 |
| 答卷请求容量与旧答卷去重回归 | 通过；12,007,031 字节 JSON 返回 201，MongoDB BSON 为 4,014,654 字节；旧账号首次重试 409，12 条历史记录完整保留 |
| `go vet ./...` | 通过 |
| Go 服务构建 | 通过；交付副本无 Git 元数据，构建仅加 `-buildvcs=false` 关闭版本戳写入 |
| `go tool cover -func=coverage.out` | **核心包合并语句覆盖率 85.6%** |
| Chromium E2E | **7/7 通过**，包含 1 个父测试和 6 个业务子场景；无跳过或失败 |

本轮使用独立的真实 MongoDB 服务与已安装的浏览器，没有在该环境执行 Docker Compose 启动流程。实际命令的业务参数如下（`go` 和 `node` 为上述版本）：

~~~sh
export INTEGRATION_MONGO_URI='mongodb://127.0.0.1:37018/?directConnection=true'
go test -count=1 -race -v -coverpkg=./internal/... -coverprofile=coverage.out ./tests/unit ./tests/integration
node tests/frontend/frontend.test.cjs
go vet ./...
go build -buildvcs=false -o bin/survey-lab ./cmd/server
go tool cover -func=coverage.out
RUN_E2E=1 PLAYWRIGHT_EXECUTABLE_PATH=/tmp/survey-review-runtime/chrome-headless-shell-linux64/chrome-headless-shell go test -count=1 -race -v ./tests/e2e
~~~

语句覆盖率的范围由 `-coverpkg=./internal/...` 确定，汇总工具为 `go tool cover`；前端测试及浏览器场景数另行统计。覆盖率用于说明本轮测试触及的 Go 语句范围，不等同于全部功能和边界已经穷尽。

## 5. 历史执行中遇到的环境问题

开始移除 Python 辅助脚本后，`make integration` 曾连接 `127.0.0.1:27018` 失败。原因是 MongoDB 容器尚未启动，后来在 Makefile 中加入自动启动与健康检查，并在测试支持代码中增加连接预检。重新运行后，数据库测试通过。

首次执行 `make e2e` 时，Node.js 报缺少 `playwright` 模块；安装项目的 npm 依赖和 Chromium 后重新执行，浏览器测试 7/7 通过。这些错误属于环境准备问题，不应写作问卷业务逻辑测试失败。
