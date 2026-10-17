# 测试用例与执行记录（第一阶段）

依据《大作业一》“至少六类测试”的要求，分别列出**步骤、输入、预期输出和结果**。自动化用例主要位于 tests/unit、tests/integration；界面测试位于 tests/frontend、tests/e2e。

**执行结果说明：** 当前整理工作能够检查仓库中的测试代码，但没有获得这次改动之后的 Go 1.26、MongoDB 和 Chromium 实测日志。下表的“未复测”是当前事实，不等于测试失败，更不应擅自改成“通过”。完成验收后按真实输出更新该列。

## 一、六类课程必需测试

| 编号与功能 | 步骤 | 代表输入 | 预期输出 | 本次结果 | 对应代码 |
| --- | --- | --- | --- | --- | --- |
| T01 创建问卷 | A 注册并登录 → POST /api/surveys → GET /api/surveys；B 登录后尝试访问 A 的问卷 | title=课程反馈、questions=[] | 创建 201、状态 draft、列表出现；B 不能读取 A 的私有问卷 | 未复测 | tests/integration/http_test.go |
| T02 添加题目 | A 在草稿中添加题目 → PUT 保存 → GET 读取；用旧 revision 再次保存 | 单选、多选、fill/text、fill/number；当前 revision | 合法题目持久化；保存 200；旧 revision 返回 409 | 未复测 | tests/unit/domain_test.go；tests/integration/http_test.go |
| T03 条件跳转 | 设置规则并发布 → POST /next；分别给出命中、不命中的答案；尝试提交跳过题答案 | 单选 eq、多选 contains_all、数字填空 gte；target=后续题/END | 返回正确 nextQuestionId；未命中进入下一题；伪造路径被 400 拒绝 | 未复测 | tests/unit/domain_test.go；tests/integration/http_test.go |
| T04 答案校验 | 发布带约束的问卷 → 用非法值调用 /next 或 /responses → 改为合法值 | 必答题空值、多选少于下限、文本过长、年龄 121；合法年龄 21 | 非法答案 400；符合题型及范围时继续或提交成功 | 未复测 | tests/unit/domain_test.go；tests/integration/http_test.go |
| T05 提交问卷 | B 登录 → POST /responses → 相同账号再次提交 → 检查 MongoDB 记录 | allowAnonymous=true、allowRepeat=false、anonymous=true、合法 answers | 首次 201；同账号再次提交 409；只保存一份答卷，匿名答卷不含 userId | 未复测 | tests/integration/http_test.go |
| T06 查询统计 | 多个账号提交已知答案 → A 查询整卷统计和指定题目统计 → 翻页查看填空 | 单选与多选不同选项、若干数字及文本答案 | totalResponses、answered、选项 count、数字 average 与原始 values 正确；分页范围正确 | 未复测 | tests/integration/mongo_test.go；tests/integration/http_test.go |

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

## 三、2026-10-17 本机日志及后续修复

依据提交者提供的终端记录，在“启动脚本去 Python 化”之后，曾得到以下真实结果：

| 命令 / 项目 | 已有终端记录 | 解释 |
| --- | --- | --- |
| `go test -count=1 ./tests/unit` | 通过 | Go 单元测试执行成功 |
| `node --test tests/frontend/frontend.test.cjs` | **31/31 通过** | 前端测试执行成功 |
| `make integration` | 未通过 | 7 个 MongoDB 测试尝试连接 `127.0.0.1:27018` 时被拒绝，没有进入业务断言 |
| `make verify` | 在集成测试阶段中止 | 与上一条相同；后面的静态检查和构建没有在这次命令中执行 |
| `coverage.out`（当时 34.3%） | **不能作为有效覆盖率** | MongoDB 测试没有成功执行，统计不完整 |

根因是旧 Makefile 仅提供默认 MongoDB URI，没有负责启动数据库。修复后，使用默认 URI 的 `make run`、`make integration`、`make e2e` 会先通过 Docker Compose 启动并等待 MongoDB 健康；对自定义数据库则跳过 Compose。测试支持代码增加了简短的连接预检，避免连续长时间等待，并且仅在集成测试成功时保留完整覆盖率文件。

**以上并不是修复后的测试通过记录。** 本次修改后的真实 MongoDB 集成结果、Chromium 验收和覆盖率仍需重新执行并填写；未执行的用例不得改写为“通过”。

## 四、最终验收记录（提交前填写）

| 项目 | 当前状态 |
| --- | --- |
| 修复版执行日期与 Go/MongoDB 版本 | 待记录 |
| 修复版 `make test` | 待复测 |
| 修复版 `make integration` | 待复测 |
| 修复版 `make verify` | 待复测 |
| 可选 `make e2e` | 待复测 |
| T01～T06 的真实输入与输出 | 待核对 |
| 问卷创建、跳转、提交、统计页面截图 | 待添加 |
| 最终覆盖率 | 仅在集成测试全部通过后记录 |

此前 2026-10-16 的历史通过记录发生在部分题型调整前，不应冒充当前代码的复测结果。请保存本次执行终端输出，按实际情况填写以上字段。
