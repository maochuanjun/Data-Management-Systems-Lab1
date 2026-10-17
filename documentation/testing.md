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

先启动 MongoDB。默认连接 Docker Compose 映射到本机 27018 端口的实例；其他地址用 `MONGO_URI` 或 `INTEGRATION_MONGO_URI` 环境变量覆盖：

~~~sh
docker compose up -d
~~~

执行快速测试（Go 业务逻辑和 Node.js 前端回归）：

~~~sh
make test
~~~

执行包含真实 MongoDB 的集成测试：

~~~sh
make integration
~~~

如果已经安装 Playwright 和 Chromium，可以再执行浏览器测试：

~~~sh
npm install
npx playwright install chromium
make e2e
~~~

完整补充检查：

~~~sh
make verify
~~~

**环境约束：** go.mod 要求 Go 1.26.0；integration 和 e2e 需要 MongoDB；前端回归需要 Node.js；浏览器验收另外需要 Playwright/Chromium。Makefile 直接执行 Go/Node 命令，不再需要 Python。`.env` 不会被自动加载；若使用该文件，请先在 shell 中导出其中的变量（参见根目录 README）。缺少这些工具时，应注明“未执行及原因”，而不是以静态代码检查代替运行结果。

## 三、验收结果填写

正式提交时，请将上一节 T01～T06 的“未复测”改为真实结果，并保留下列可核对信息：

| 项目 | 本次记录 |
| --- | --- |
| 执行日期及环境 | 尚未记录 |
| make test | 未在本轮执行 |
| MongoDB 集成测试 | 未在本轮执行 |
| Chromium 端到端测试 | 未在本轮执行 |
| 问卷创建、跳转、提交、统计页面截图 | 尚待实际运行后添加 |
| 失败用例及修复后复测 | 以本次实际日志为准 |

此前仓库文档记载过 **2026-10-16** 的本地测试通过情况，但部分记录早于最新题型结构变更。因此不将历史覆盖率、用例通过数量或旧浏览器记录写作本轮成果。若最终版本复测通过，应以最终提交对应的日期、命令与输出替换上述说明。
