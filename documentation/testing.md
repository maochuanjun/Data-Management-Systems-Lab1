# 测试分层与执行记录

所有测试集中在 `tests/`，通过公开领域、HTTP、存储接口验证行为。生产代码未为测试导出私有函数或数据库字段。

## 目录与作用

| 目录 | 方法 | 验证内容 |
| --- | --- | --- |
| `tests/unit` | Go 表驱动单元测试、HTTP Recorder | 问卷及四题型校验、跳转、必答约束可满足性、精确选择数量、路径重算、JSON/跨站保护、分页参数 |
| `tests/integration` | 真实 MongoDB + HTTP 服务 + Cookie 客户端 | 权限、生命周期、匿名、重复策略、同账号并发去重、不同账号并发、统计、稳定分页、600份万字文本容量回归、Schema |
| `tests/frontend` | Node 内置测试框架 + 轻量 DOM/事件替身 | 直接执行 app.js，验证特殊 ID、保存锁、失败恢复、翻页和过期响应；这部分不是浏览器测试 |
| `tests/e2e` | Playwright 驱动真实 Chromium + Go HTTP 服务 + MongoDB | 真正点击网页注册、编辑四题型、发布、填写校验、跳转、匿名提交、拒绝重复、统计分页、关闭和移动视口 |
| `tests/fixtures/v1` | 固定 Extended JSON 历史数据 | 旧用户、草稿、已发布问卷、答卷可读；新旧数据可共同使用，旧答卷不被改写 |
| `tests/support` | 共用隔离数据库夹具 | 随机创建本次测试库、检查原始文档、结束时只清理该库 |

此前 Go 测试与业务代码同目录，前端回归位于 tests/ 根目录，浏览器验证手工触发；现在采用快速单测、真实数据库集成、可重复浏览器验收三层组合。前端替身用于高频失败及竞态回归，浏览器验收验证它无法模拟的真实 DOM 与操作。

## 运行

```sh
make test
python3 scripts/with-local-mongo.py make integration
python3 scripts/with-local-mongo.py make e2e
python3 scripts/with-local-mongo.py make verify
```

`make test` 不需要 MongoDB，执行单元测试和22项前端回归。`make integration` 在缺少 `INTEGRATION_MONGO_URI` 时失败；提供后执行全部单元和真实数据库集成，开启 Go race detector，并用 `-coverpkg=./internal/...` 统计跨包覆盖率。报告为忽略的 `coverage.out`，查看 `go tool cover -func=coverage.out`。

`make e2e` 也要求数据库 URI，并开启真实浏览器用例；平常的 `go test ./...` 会跳过该 opt-in 用例。`make verify` 依次执行集成、E2E、go vet 和构建；它需要完整的开发测试工具，不能用快速测试结果替代。

### 浏览器工具

E2E 使用已有的 Node.js、Playwright Library 和 Chromium，不增加服务运行时依赖、不自动安装软件。`scripts/with-browser-tools.py` 尝试本地模块、全局/缓存安装和已有浏览器。可显式指定：

```sh
export PLAYWRIGHT_MODULE=/absolute/path/to/node_modules/playwright
export PLAYWRIGHT_EXECUTABLE_PATH=/absolute/path/to/chromium
python3 scripts/with-local-mongo.py make e2e
```

建议使用与 Playwright 匹配的浏览器版本。找不到工具时命令明确失败，不把未执行记作通过。参考 [Playwright Library](https://playwright.dev/docs/library)。

### 数据隔离

`tests/support.NewMongo` 创建随机 `questionnaire_test_*` 库。HTTP 和浏览器服务仅存在于测试中，用随机端口；测试结束关闭服务和连接，只删除该次夹具分配的数据库。固定 v1 数据只导入测试库，不导入实际项目库。

## 本轮实际结果（2026-10-15）

| 检查 | 实际结果 |
| --- | --- |
| 整理前原有修复的 make integration | 真实 MongoDB + race 全部通过，原有21项前端回归通过 |
| 整理后 make integration | 单元、真实 MongoDB、容量、兼容与并发回归全部通过，22项前端回归通过，无 race 报告 |
| 跨包语句覆盖率 | internal/domain、store、web 合计85.6%，不包含 main 与测试辅助代码 |
| make e2e | 5个实际浏览器业务场景通过；无 JavaScript 异常，390px统计页无横向溢出 |
| 静态检查与构建 | go vet、Go构建、JS语法和Python辅助脚本语法通过 |

### 关键验收用例

| 用例 | 输入与步骤 | 预期与实测 |
| --- | --- | --- |
| 创建及添加题目 | 注册，创建草稿，添加四题型，携带revision更新 | 保存成功；旧revision返回409；发布后不可修改 |
| 精确多选 | minSelect=maxSelect=2，分别给1、2、3项及重复项 | 仅2个不同合法选项通过；单元及真实HTTP验证通过 |
| 单选/多选/数字跳转 | 配置eq、contains、contains_all、数值比较及END | 首条命中优先；跳过必填有效；伪造路径答案被拒绝 |
| 校验 | 必答空值、非法选项、Unicode长度、数字范围与整数、无解必答配置 | 非法配置或答案拒绝，合法边界通过 |
| 同账号并发 | 对禁止重复问卷同时发12次提交 | 恰好1次成功，其余409 |
| 不同账号并发 | 8个登录账号同时首次提交同一问卷 | 8份全部成功，存储数及统计数为8，再次提交被拒绝 |
| 匿名与重复 | 匿名/实名共用去重机会；另一个问卷允许同账号提交5次 | 匿名不保存userId；去重有效；允许重复时5份均保存 |
| 统计 | 已知选择答案和数字1/3/5/0、跳过值、零值 | 汇总覆盖全部回答；分页不影响均值和选项次数 |
| 容量与分页 | 600份每份10000汉字，首末页；同时间戳不同ID | 不再触发单个BSON16MiB故障；原文不截断，排序稳定 |
| 固定v1兼容 | 导入固定历史JSON，重复Init，读取、发布、填写和统计 | 旧数据可用，新旧答案均值15，原答卷完全保留 |
| 新回答期间翻页 | 初始21份均值11，翻页时收到22份均值15与新答案 | 同步更新当前题汇总及总数；此新增回归先失败后通过 |
| 关闭及截止 | 发布后关闭，或把测试库中的deadline置过去 | next和提交返回409；浏览器显示关闭提示 |
| 浏览器分页 | 真浏览器提交特殊ID文本，补齐22份，20/2条翻页 | 完整可遍历，均值11.5，上一页/下一页正常 |

这些是功能、回归、并发与一个特定容量问题的验收，不能当作全面生产负载、安全审计或所有浏览器兼容性结论。历史排查过程保留在 [审查记录](audit-2026-10-15.md)。
