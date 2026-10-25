# 测试用例与结果

测试覆盖创建问卷、添加题目、条件跳转、输入校验、提交问卷、统计结果，以及题库与版本管理。单元测试使用 Go `testing`，数据库集成测试使用独立的 MongoDB 测试库，前端使用 Node.js 测试，浏览器验收使用 Playwright/Chromium。

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

## 3. 历史基线执行结果（2026-10-18）

以下是第一阶段的执行记录。

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

## 4. 历史执行中遇到的环境问题

开始移除 Python 辅助脚本后，`make integration` 曾连接 `127.0.0.1:27018` 失败。原因是 MongoDB 容器尚未启动，后来在 Makefile 中加入自动启动与健康检查，并在测试支持代码中增加连接预检。重新运行后，数据库测试通过。

首次执行 `make e2e` 时，Node.js 报缺少 `playwright` 模块；安装项目的 npm 依赖和 Chromium 后重新执行，浏览器测试 7/7 通过。这些错误属于环境准备问题，不应写作问卷业务逻辑测试失败。

## 5. 题库与版本功能的回归范围

新增测试继续放在 `tests/`，直接复用原有服务、测试数据库与 HTTP 客户端支持。领域单元测试隔离检查规则，集成测试通过真实 MongoDB 验证持久化、权限、并发和聚合；前端测试检查交互逻辑，浏览器测试覆盖用户实际操作链路。

| 场景 | 检查点 | 主要测试 |
| --- | --- | --- |
| 复用与版本隔离 | 同一版本选入不同问卷；发布后派生新版本，旧问卷快照不变 | `TestLibraryReuseImmutableHistoryAndRestore` |
| 历史、分支与恢复 | 从历史版本派生；保留父版本；恢复生成新版本及恢复来源 | 同上 |
| 指定用户共享 | 作者授权、接收者使用、第三人无权；撤销后不能新增引用，原快照仍有效；篡改来源内容被拒绝 | `TestLibrarySharingRevocationAndSnapshotIntegrity` |
| 题库固定项 | 创建、添加/移除/替换版本，检查旧修订冲突；删除题库后版本仍在 | `TestLibraryQuestionBankCRUDPinsAndConflicts` |
| 使用情况与统计权限 | 作者只能看到他人引用总量；所有人的答案统计只来自本人问卷 | `TestLibraryUsagePrivacyAndWeightedNumericStats` |
| 数值汇总与分布 | 不同问卷不同样本数，验证全部答案的平均值；分桶回答数与实际回答数一致 | 同上 |
| 版本隔离及分页 | 不同题型版本分别统计；限定版本后原始答案分页，非法分页参数被拒绝 | `TestLibraryStatsVersionSeparationAndRawPagination` |
| 并发版本 | 并发创建版本，编号唯一、父版本关系保留，历史不会被覆盖 | `TestLibraryConcurrentVersionAllocation` |
| 创建中断恢复 | 完整 v1 的待就绪题目恢复可见，缺少 v1 的题目仍隐藏，重复初始化安全 | `TestLibraryInitRecoversOnlyCompletePendingFamilies` |
| 版本分配空号 | 模拟已分配编号但版本未插入，读取与后续创建仍使用实际存在的最新版本 | `TestLibraryVersionAllocationGapsKeepLatestReadable` |
| 长文本集合 | 600 条各含 10,000 个汉字的回答，统计总量准确、页面有界，避免大关联结果失败 | `TestLibraryStatsLargeTextDatasetRemainsPaged` |

`tests/unit/library_test.go` 覆盖可复用题目校验、内容比较及题库约束，重点检查跳转规则/来源不能嵌套、不可修改的内容字段、重复固定项和长度上限。历史数据兼容、匿名去重、长答卷、原统计、归档与重开继续由既有测试保护。

运行全部新增与既有核心测试沿用 `make verify`，浏览器测试沿用 `make e2e`。只定位题库集成测试时，可执行：

~~~sh
go test -count=1 -race -v ./tests/integration -run '^TestLibrary'
~~~

## 6. 第二阶段首次验收记录（历史，2026-10-22）

本节对应第二阶段首次实现后的验证，后续共享并发、草稿保护及本次复核分别记录在以下各节。保留当时的环境、数量和耗时，不把这些结果当作最新提交的重新执行。

| 检查 | 已获得的实际证据 |
| --- | --- |
| `make test` | Go 单元测试通过；直接运行 Node 测试确认前端 31/31 通过 |
| `make vet`、`make build` | 通过 |
| Chromium E2E 与 Go `-race` | 原有 6 个业务场景和新增 6 个业务场景全部通过；含 2 个父测试共 14 项，Go E2E 执行 16.617 秒，无页面脚本错误 |
| 真实 MongoDB 集成与 `-race` | 完整 `make integration` 通过：18 个单元、19 个集成顶层测试（含新增 9 个题库集成测试，子测试另计）；单元包 6.274 秒、集成包 49.294 秒 |
| 并发初始化专项 | 修复后以 `-count=5 -race` 重复 5 次全部通过，执行 2.386 秒 |
| `go tool cover -func=coverage.out` | 核心包合并语句覆盖率 **83.1%**，统计范围为 `./internal/...` |

首轮数据库测试发现并发 `Init()` 时，Schema 更新可能与另一个初始化过程正在创建的索引冲突。修复限定为 MongoDB 索引构建冲突错误的可取消重试，随后复跑既有并发初始化回归及全套集成测试，修复后完整集成测试通过。

## 7. 共享并发修复与回归（历史，2026-10-22）

本轮修复共享旧页面覆盖、版本保存响应清掉后续草稿、共享保存期间输入与实际授权名单不一致三个问题。新增回归如下：

| 测试文件 | 新增覆盖 |
| --- | --- |
| `tests/integration/sharing_test.go` | 3 个顶层测试：撤销权限后拒绝旧名单；缺失/null/非法版本号与作者权限；新旧文档并发更新仅一方成功、旧字段兼容及连续更新。共享版本独立于题目内容版本 |
| `tests/frontend/frontend.test.cjs` | 12 项：保存、恢复、共享共用忙碌锁；失败保留输入并解锁；重复提交；成功同步名单和版本号；冲突不自动重试；离开页面及详情刷新期间导航的过期响应隔离 |
| `tests/e2e/library_concurrency.test.cjs` | 4 个真实 Chromium 场景：版本保存/恢复期间阻止其他编辑、共享保存锁定与规范化名单、两页撤销后旧页面保存返回 409。使用实际 HTTP 服务和 MongoDB，以响应屏障控制时序 |

先在修复前源码上验证失败：前端首批 10 项新增回归失败；4 项浏览器场景均因实际缺陷失败，包括旧页面保存返回 200 而非预期 409。修复后的完整测试结果：

| 检查 | 结果 |
| --- | --- |
| Go 单元与真实 MongoDB 集成，`-count=1 -race -v` | 40 个顶层测试通过，无失败、无跳过；集成包 37.692 秒 |
| 前端 Node 测试 | 43/43 通过 |
| Chromium E2E，Go `-race` | 16 个业务场景通过，含 3 个父测试共 19 项；Go E2E 13.497 秒 |
| `go vet ./...`、服务构建、JavaScript 语法检查、`git diff --check` | 通过 |
| 核心包合并语句覆盖率（`./internal/...`） | 83.3% |

共享接口新增必填 `sharingRevision`，调用方应按更新后的 API 文档发送；已有数据库无需批量迁移。

## 8. 详情页未保存草稿保护（历史，2026-10-22）

新增 11 项前端回归和 3 项 Chromium 场景。修复前，首批新增前端回归中 8 项因没有丢弃确认而失败，原有 43 项仍通过；独立复核另补默认值等价回归，复现了填空类型往返切换的误提示。修复后 54 项全部通过。

- 恢复版本分别检查未保存标题、选项、约束、版本说明和共享名单。取消确认后不发送恢复请求，输入保留；确认后生成新版本，共享名单不会被顺带保存。
- 切换版本基线、取消编辑前提示丢弃草稿；拒绝操作后仍可继续编辑。空白未改动或修改后恢复原值时不提示，约束键顺序、默认整数开关以及空选项的表示差异不误报；已有约束在切换题型时被清掉仍会提示。
- 保存新版本前保护未保存共享名单；拒绝后保留两份草稿。保存共享不会清空版本草稿，成功同步共享基线后可直接提交版本。

| 本次验证 | 结果 |
| --- | --- |
| `node tests/frontend/frontend.test.cjs` | 54/54 通过 |
| 真实 MongoDB + Chromium，Go `-race` | 19 个业务子场景通过，含 3 个父测试共 22 项，无跳过；Go E2E 13.241 秒 |
| `make test vet build` | Go 单元测试、前端测试、静态检查和构建通过 |
| 三个改动 JavaScript 文件的 `node --check` | 通过 |

复现命令沿用 `make test` 和 `make e2e`，也可运行 `RUN_E2E=1 go test -count=1 -race -v ./tests/e2e`。

## 9. 需求复核与迟到响应验收（历史，2026-10-22）

本节复核起点是 `improve` 的 `3de80c8ff535f4054185481a4355a55df320ddab`，完成于 `59ee1ce594661e8dff960e7762959da161712c24`。由 ChatGPT 协助在独立环境读取需求、审查代码、复现问题并运行修复后的测试。本节的 40/66/22 项及 83.3% 是该轮实际结果；后续用户反馈修复的最终结果见第 10 节。

### 异步页面回归

新增题目、创建题库、保存和删除题库时，如果用户已离开页面，迟到的成功或失败响应不应再次导航或改写当前通知。跨问卷统计切换版本或离开页面后，旧查询的错误也不应覆盖新结果。本次在 `tests/frontend/frontend.test.cjs` 增加 12 项受控时序回归：前四类写操作分别检查成功、失败两种响应，共 8 项；统计检查切换版本与页面导航，共 2 项；问卷题目另存和题库选题预览各检查 1 项迟到成功响应。

先将扩展后的 66 项测试用于修复前源码，55 项通过、11 项失败；新增回归中的 1 项确认已有防护，其余 11 项复现上述问题。修复后原有 54 项及新增 12 项共 66 项全部通过。

### 第二阶段固定数据与断言

`TestLibraryUsagePrivacyAndWeightedNumericStats` 通过真实 HTTP 和 MongoDB 检查以下输入。同一版本在问卷 B 中出现两次，各次出现使用不同的局部题目 ID。

| 数据来源 | 提交内容 | 本人统计的贡献 |
| --- | --- | --- |
| 本人问卷 A，引用一次 | 一份答卷：100 | 1 个回答实例。 |
| 本人问卷 B，引用两次 | 三份答卷：10/11、20/21、30/31 | 6 个回答实例。 |
| 另一用户的问卷，引用共享题目 | 一份答卷：999 | 不进入当前用户的答案统计。 |

当前用户的预期为 `totalAnswers=7`、`surveyCount=2`、平均值 `223/7`，分布各桶计数之和为 7。另一用户只统计自己的 999。题目作者查询使用情况时，全局引用问卷数为 3、出现次数为 4，本人明细数组有 3 项；另一用户问卷的名称与 ID 不返回。该例同时验证样本加权、重复出现、问卷去重计数和权限隔离。

`TestLibraryReuseImmutableHistoryAndRestore` 另固定版本关系：v2 与 v3 均从 v1 派生；恢复 v1 生成 v4，v4 的父版本是恢复时的 v3，恢复来源是 v1。读取 v1 和已保存问卷后逐项比较内容，确认旧版本、问卷快照与历史回答的解释没有被覆盖。

### 独立深分页检查

本次另设存储层审查探针：4,000 条接近 10,000 个 Unicode 码点的中文答卷，原文合计接近 120 MB；查询 `offset=3950, limit=50`，应按稳定顺序完整返回最后 50 条，同时 `answered=4000`、`hasMore=false`。原实现在真实 MongoDB 与 Go `-race` 下通过该检查。

### 最终执行结果

| 检查 | 本次最终结果 |
| --- | --- |
| Go 单元与真实 MongoDB 集成，启用 `-race` | 18 个单元与 22 个集成顶层测试全部通过，共 40 个；含子测试共 192 条 PASS 记录，0 失败、0 跳过。 |
| JavaScript 前端测试 | 66/66 通过：原有 54 项，本轮新增 12 项；按 `node tests/frontend/frontend.test.cjs` 的实际业务测试计数。 |
| 真实 Chromium E2E，Go `-race` | 22/22 通过，含 3 个父测试与 19 个业务子场景，0 失败、0 跳过；Go 包装入口的 1 项测试通过，不重复计入浏览器场景总数。 |
| 静态检查与构建 | `go vet ./...` 与服务端构建通过。 |
| `internal/...` 合并语句覆盖率 | 83.3%，由合并后的同一份 `coverage.out` 经 `go tool cover` 汇总。 |

标准复现入口仍为 `make test`、`make integration`、`make vet`、`make build` 和 `make e2e`；使用外部测试数据库时设置 `INTEGRATION_MONGO_URI`。浏览器验收先按 README 安装 npm 依赖及 Chromium。交付包保留本轮实际命令、环境和逐条输出；覆盖率只汇总同一份覆盖率数据，Node 和浏览器场景数单独计数。既有历史数据兼容、匿名防重、长答卷、问卷生命周期和六类第一阶段测试继续执行。

## 10. 七项用户反馈修复与最终验收（2026-10-22）

从 `59ee1ce594661e8dff960e7762959da161712c24` 继续，对个人截图中七项填写与编辑问题核实、修复并回归。八项第二阶段正式需求仍由既有测试检查，新增交互要求不冒充需求文档原文条款。

### 反馈与断言对应

| 反馈 | 回归步骤与必要断言 | 测试位置 |
| --- | --- | --- |
| 回退丢失输入 | 当前文本/数字尚未按下一步就回退，再回来原内容保持；数字 0、`-`、`1e` 等草稿不被清空；非法数值下一步仍失败。单选、多选也保留。改变分支后只提交实际路径中的合法答案。 | 前端 30 项新增回归中的填写场景；`tests/e2e/feedback.test.cjs`。 |
| 未保存离开 | 问卷及题库编辑、新建题目/题库、版本与共享草稿、未提交回答均触发相应保护；取消站内导航、浏览器 Back、刷新后输入保留。保存成功更新基准，失败保留草稿；等价截止时刻不误警告；退出等待期不能新增将被丢弃的输入。 | 前端回归；真实浏览器对站内导航、后退和原生刷新分别检查取消与确认。 |
| 重复提示太晚 | 不允许重复时当前账号的提前状态正确；匿名/实名同键、账号隔离、允许重复跳过查询；查询失败不误报可填；最终重复 POST 仍 409。 | `TestFeedbackPublicSubmissionPrecheck`、`TestFeedbackSubmissionPrecheckIdentityAndRepeatPolicy`、`TestFeedbackLegacySubmissionPrecheck`；旧/新浏览器防重断言。 |
| 相同选项可发布 | 单选、多选的新配置拒绝首尾 ASCII/Unicode 空白去除后相同标签；保存、发布、新建与派生版本均拒绝。旧已发布快照仍可填，旧版本可忠实恢复且保持原文，修正派生后可发布。 | `TestFeedbackDuplicateOptionLabelsRejected`、`TestFeedbackPublishedDuplicateLabelsRemainAnswerable`、`TestFeedbackChoiceConfigurationAndPublishedCompatibility`。 |
| 新规则折叠 | 点击新增规则后 `details.open=true`，条件、目标可立即配置，重绘后继续展开。 | 前端与真实浏览器规则场景。 |
| 答案概览 | 只列实际路径；选项显示文字；0 正常显示；“已跳过”与合法空文本/空多选分开，旧分支不混入概览与最终提交。 | 前端与真实浏览器概览场景。 |
| 错误难定位 | 配置和作答错误显示题号与必要的选项序号，不回显随机内部题 ID；特殊合法 ID 仍保持原协议和路径语义。 | `TestFeedbackValidationUsesQuestionNumbers`、HTTP 集成及浏览器错误场景。 |

### 修复前后证据

| 同条件检查 | 修复前实际结果 | 修复后实际结果 |
| --- | --- | --- |
| 最终 96 项 Node 前端测试 | 原始 `59ee1ce` 实现 70 通过、26 失败；原有 66 项均通过，新增 30 项中 4 项确认原有正确行为。 | 96/96 通过。 |
| 新增 Go 专项，7 个顶层测试、35 条含子测试记录 | 3 通过、32 失败；3 条通过记录是旧已发布重复标签兼容。 | 35 条全部通过，启用 `-race`，集成连接真实 MongoDB。历史恢复与特殊 ID 的后续补充断言时序另记在专项说明中。 |
| 新增真实浏览器 12 个业务场景 | 2 通过、10 失败；已通过的是分支旧答案清理和允许重复填写。含父测试的 Node 数量为 13，父失败不另算业务缺陷。 | 12 个业务子场景全部通过，连同父测试为 13/13。 |

初轮前端 92 项前后日志仍保留。独立复查后又补充日期等价、退出成功/失败等待期和重新开启截止时间 4 项；92 项修复中间版在最终 96 项下为 92 通过、4 失败，随后修复并重新获得最终同套件对照。

旧 E2E 相关预期按新行为调整：添加规则后直接检查展开；已提交账号重进或问卷重新开启后仍检查提前提示，另外用新账号验证重开后可填写。继续保留直接提交 409、统计数量以及历史两份答卷保留断言。取消原生刷新的测试助手改为等待实际对话框后验证页面和输入，避免把被取消导航的等待超时当成产品失败。环境端口受限的启动错误、旧预期和测试助手问题均单列说明，不混入最终缺陷或失败数量。
