# 测试用例与执行记录

## 1. 测试范围与目录

| 路径 | 运行方式 | 覆盖内容 |
| --- | --- | --- |
| `tests/unit/` | Go 表驱动测试、Python `unittest` | 题目结构、跳转规则、校验边界、提交路径、HTTP 参数、配置读取 |
| `tests/integration/` | Go + 真实 MongoDB + HTTP | 注册登录、权限、问卷生命周期、匿名/重复、统计、并发、旧数据兼容 |
| `tests/frontend/` | Node.js 原生测试器 | 网页输入、特殊题目 ID、保存锁、提交过程回退、统计翻页与错误恢复 |
| `tests/e2e/` | Playwright + Chromium + Go 服务 + MongoDB | 注册、创建、发布、填写、提交、统计、关闭的真实网页操作 |
| `tests/fixtures/v1/` | 集成测试读取固定历史 JSON | 验证第一阶段数据在后续修改中仍可读取 |

本项目测试库名使用随机 `questionnaire_test_*`，完成后删除本次测试库；不要在真实数据库上手工复用测试脚本中的数据库删除语句。

## 2. 执行方法

```sh
# 仅 Go/Python/Node 快速测试
make test

# 以下命令需要项目根目录 .env 中配置 MONGO_URI
python3 scripts/with-local-mongo.py make integration
python3 scripts/with-local-mongo.py make e2e
python3 scripts/with-local-mongo.py make verify
```

`make integration` 开启 Go race detector，调用真实 MongoDB 测试；`make e2e` 需要事先安装的 Playwright 和 Chromium。`make verify` 依次执行集成、浏览器、`go vet` 和构建。未设置数据库或未安装浏览器时应记录“未执行/失败”，不能写成“通过”。

## 3. 第一阶段必需用例

下面列出教师要求的六类功能及可重复步骤。2026-10-16 在真实 MongoDB、Chromium 和本地 HTTP 服务上运行的完整测试已通过；下表“已通过”表示相应自动化用例在此次验收中通过。

| 编号 | 类别及操作步骤 | 主要输入 | 预期输出 | 已有依据 |
| --- | --- | --- | --- | --- |
| T01 | 注册用户 → 创建问卷草稿 → 读取问卷列表 | 标题“课程反馈”、空题目 | 返回 `201`，状态 `draft`；仅创建者可查看 | HTTP 集成测试，本次已通过 |
| T02 | 在草稿中添加单选、多选、文本、数字题 → 保存 | 四题型、选项 ID、`revision` | 保存成功；旧 revision 返回 `409`；发布后禁止编辑 | 单元、HTTP 和 E2E，本次已通过 |
| T03 | 设定单选/多选/数字规则 → 填写并跳转 | `eq`、`contains_all`、`gte`、`END` | 按首条命中规则向后跳；未命中顺序前进 | 单元、HTTP 和 E2E，本次已通过 |
| T04 | 输入不合法答案再改成合法答案 | 必答空值、多选少于2项、文本长度超限、年龄 121/21 | 非法值返回 `400` 并提示；合法值通过 | 单元、HTTP 和 E2E，本次已通过 |
| T05 | 两个账号填写并提交；再次使用同一账号提交 | 合法答案、匿名标记、`allowRepeat=false` | 首次 `201`，重复 `409`；匿名不保存 `userId` | HTTP/数据库/E2E，本次已通过 |
| T06 | 创建者查看整卷与单题统计，验证平均值和选项次数 | 已知单选/多选/数字答案 | 选项次数、回答份数和平均值符合预期；分页可遍历文本内容 | MongoDB/HTTP/E2E，本次已通过 |

## 4. 额外边界与回归

| 测试内容 | 输入或步骤 | 预期 |
| --- | --- | --- |
| 所有权 | B 用户请求 A 用户的私有问卷、统计 | 返回 `404` |
| 并发去重 | 同一账号对禁止重复的问卷并发提交 12 次 | 恰有一次成功，其余 `409` |
| 不同人并发 | 8 个账号同时填写同一问卷 | 8 份均可保存 |
| 不可能的必答配置 | 必答文本 `maxLength=0`，或整数范围 `0.1~0.9` | 草稿或发布时拒绝 |
| 容量与分页 | 单题 600 份、每份一万汉字；按偏移查询 | 汇总不超过 BSON 单文档限制；可读取首末页 |
| 特殊题目 ID | `__proto__`、`constructor` 等 | 答案可正常输入、序列化、提交 |
| 旧数据兼容 | 导入 `tests/fixtures/v1`，重新初始化集合、发布和提交 | 旧数据未被更改；旧问卷仍可填写和统计 |
| 本轮新增：提交过程回退 | 延迟提交响应、尝试点击“上一步”、模拟成功/失败 | 提交中禁用回退，失败时恢复，不丢失填写 |
| 本轮新增：只读汇总 | 请求 `?summaryOnly=true` 和非法参数 | 汇总保留计数与均值，`values=[]`；非法值返回 `400` |
| 本轮新增：Unicode 密码 | 少于8个汉字字符，以及8个汉字字符 | 前者 `400`，后者可注册 |

## 5. 已验收的第一阶段基线（2026-10-16）

开发者在 `coursework/phase1-cleanup-report` 分支上实际执行了 `make test`、独立的 `make e2e`、完整的 `python3 scripts/with-local-mongo.py make verify` 和覆盖率汇总命令。以下数据来自终端日志，不是预估值。

| 检查 | 命令或工具 | 实际结果 |
| --- | --- | --- |
| Go 单元测试 | `go test -count=1 ./tests/unit` | 通过 |
| Python 配置测试 | `python3 -m unittest discover -s tests/unit -p '*_test.py'` | 6 项通过，0 失败 |
| Node 前端测试 | `node --test tests/frontend/frontend.test.cjs` | 24 项通过，0 失败 |
| 真实 MongoDB 集成 | `go test -count=1 -race -coverpkg=./internal/... -coverprofile=coverage.out ./tests/unit ./tests/integration` | 两个 Go 测试包通过；未报告数据竞争 |
| Chromium E2E 单独复测 | `make e2e` | 5 个业务子场景通过（Node 测试器汇总 6/6，包括父测试） |
| 完整验收 | `python3 scripts/with-local-mongo.py make verify` | 成功完成数据库集成、Python/Node 测试、Chromium E2E、`go vet ./...` 与 Go 构建 |
| 核心包合并语句覆盖率 | `go tool cover -func=coverage.out` 的 total 行 | **86.1%** |

Go 测试日志单独显示的 `36.4%`（单元测试包）与 `80.3%`（集成测试包）不是整个项目的汇总值；**86.1%** 才是 `coverage.out` 的 total 行。此覆盖率统计范围由 `-coverpkg=./internal/...` 指定，不包括所有程序入口及外部工具。

### 5.1 本轮问题与修复验证

第一次完整验收时，前端与 MongoDB 测试已经通过，但 E2E 的“特殊题目 ID 与分页”场景在点击“查看填写内容”后立即读取 DOM 行数，得到 0 条而不是预期的 20 条。排查发现原始答案由异步 HTTP 请求加载，断言执行得过早；因此在 `tests/e2e/browser.test.cjs` 中先等待第 20 条答案元素出现，再检查数量。

修复后先单独运行真实 Chromium E2E，通过 5 个业务子场景；随后完整执行 `make verify`，上述 E2E 再次通过，并成功运行 `go vet` 和构建。没有因测试失败而省略后续步骤。

这一结果验证了当前测试覆盖的主要功能，不等于完成生产压力测试、所有浏览器兼容性测试或完整安全审计。第二阶段仍应执行旧数据兼容与所有核心功能回归。

### 5.2 报告所需的人工证据

提交 PDF 前可补充少量真实截图，例如问卷设计界面、非法输入与跳转提示、整卷及单题统计。测试命令与结果已记录，本节无需把全部终端日志逐行复制到报告中。

## 6. 本次新增功能的验收（待执行）

以下功能由 2026-10-16 后续的“问卷管理与条件跳转”改动引入，**不同于上面已通过的基线测试**。相关用例已经加入 `tests/integration/http_test.go`、`tests/frontend/frontend.test.cjs` 与 `tests/e2e/browser.test.cjs`；尚未获得该新分支在开发者机器上执行的结果。

| 验收项目 | 输入与步骤 | 预期 |
| --- | --- | --- |
| 删除草稿 | A 创建草稿并删除；B 尝试删除 A 的草稿 | A 删除后为 404；B 操作为 404；发布过的问卷不能永久删除 |
| 草稿归档及恢复 | 归档草稿，尝试修改/发布，再恢复 | 归档时拒绝编辑和发布；恢复后草稿仍可编辑 |
| 发布问卷归档 | 发布并收到回答后归档 | 自动关闭且 `archived=true`；回答和统计不丢失，填写端无法继续答卷 |
| 恢复归档 | 对已经归档的已关闭问卷执行恢复 | 仍为 `closed`，不自动开启；可由创建者再次重新开启 |
| 重新开启 | 关闭、重新开启；旧账号再次提交 | 已有答卷、规则和防重复策略保留，旧账号不能绕过重复限制 |
| 截止时间 | 对截止时间已过的已关闭问卷重新开启 | 原时间直接复用返回 400；清除或设置未来时间后允许开启 |
| 单选分支 | 第 2 题选“男”进入第 3 题、选“女”跳过第 3 题进入第 4 题 | 规则编辑器显示“男/女”而非 ID，底层仍保存并比较正确的选项 ID |
| 多选分支 | 编辑器将两种选项勾选为 `contains_all` | 底层保存选项 ID 数组，后端按组合匹配 |

执行要求：

```sh
make test
python3 scripts/with-local-mongo.py make verify
go tool cover -func=coverage.out | tail -n 1
```

在得到实际日志之前，不得把本节新功能标成“通过”；新的覆盖率也需以本次 `coverage.out` 计算结果为准。
