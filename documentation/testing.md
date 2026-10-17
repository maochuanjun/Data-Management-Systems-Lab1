# 测试用例与执行记录（第一阶段）

按作业要求，分别列出**步骤、输入、预期输出和结果**。自动化用例主要位于 tests/unit、tests/integration；界面测试位于 tests/frontend、tests/e2e。

## 一、核心测试

| 编号与功能 | 步骤 | 代表输入 | 预期输出 | 本次结果 | 对应代码 |
| --- | --- | --- | --- | --- | --- |
| T01 创建问卷 | A 注册并登录 → POST /api/surveys → GET /api/surveys；B 登录后尝试访问 A 的问卷 | title=课程反馈、questions=[] | 创建 201、状态 draft、列表出现；B 不能读取 A 的私有问卷 | 对应自动化测试通过（测试包整体通过） | tests/integration/http_test.go |
| T02 添加题目 | A 在草稿中添加题目 → PUT 保存 → GET 读取；用旧 revision 再次保存 | 单选、多选、fill/text、fill/number；当前 revision | 合法题目持久化；保存 200；旧 revision 返回 409 | 对应自动化测试通过（测试包整体通过） | tests/unit/domain_test.go；tests/integration/http_test.go |
| T03 条件跳转 | 设置规则并发布 → POST /next；分别给出命中、不命中的答案；尝试提交跳过题答案 | 单选 eq、多选 contains_all、数字填空 gte；target=后续题/END | 返回正确 nextQuestionId；未命中进入下一题；伪造路径被 400 拒绝 | 对应自动化测试通过（测试包整体通过） | tests/unit/domain_test.go；tests/integration/http_test.go |
| T04 答案校验 | 发布带约束的问卷 → 用非法值调用 /next 或 /responses → 改为合法值 | 必答题空值、多选少于下限、文本过长、年龄 121；合法年龄 21 | 非法答案 400；符合题型及范围时继续或提交成功 | 对应自动化测试通过（测试包整体通过） | tests/unit/domain_test.go；tests/integration/http_test.go |
| T05 提交问卷 | B 登录 → POST /responses → 相同账号再次提交 → 检查 MongoDB 记录 | allowAnonymous=true、allowRepeat=false、anonymous=true、合法 answers | 首次 201；同账号再次提交 409；只保存一份答卷，匿名答卷不含 userId | 对应自动化测试通过（测试包整体通过） | tests/integration/http_test.go |
| T06 查询统计 | 多个账号提交已知答案 → A 查询整卷统计和指定题目统计 → 翻页查看填空 | 单选与多选不同选项、若干数字及文本答案 | totalResponses、answered、选项 count、数字 average 与原始 values 正确；分页范围正确 | 对应自动化测试通过（测试包整体通过） | tests/integration/mongo_test.go；tests/integration/http_test.go |

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

**环境要求：** Go 1.26+、Make；默认数据库模式还需 Docker Compose；`make test` 的前端部分需 Node.js；`make e2e` 另外需 Playwright/Chromium。默认连接与服务参数写在 Makefile 中。

## 三、2026-10-17 实际验收

| 实际操作 | 终端结果 | 结论 |
| --- | --- | --- |
| `make test` — Go 单元测试 | `ok questionnaire/tests/unit` | 通过 |
| `make test` — JavaScript 前端测试 | `tests 31; pass 31; fail 0` | **31/31 通过** |
| `make integration` — MongoDB 健康检查 | `Container ...mongodb-1 Healthy` | 通过 |
| `make integration` — Go 单元与集成测试 | 两个测试包均为 `ok` | 通过 |
| `make verify` — 核心测试复跑 | 前端 31/31，Go 单元及 MongoDB 集成包均为 `ok` | 通过 |
| `make verify` — `go vet ./...` | 正常执行，无错误输出 | 通过 |
| `make verify` — `go build -o bin/survey-lab ./cmd/server` | 正常结束，无报错 | 通过 |
| `go tool cover -func=coverage.out \| tail -n 1` | `total: (statements) 85.7%` | **核心后端包合并语句覆盖率 85.7%** |
| 安装 `npm install` 与 `npx playwright install chromium` 后执行 `make e2e` | Chromium 六项业务子场景均成功；`tests 7; pass 7; fail 0`；Go 包 `PASS` | **7/7 通过（含父测试）** |

相关测试日志如下：

```log
~/WorkSpace/Data-Management-Systems-Lab1
❯ make verify
go test -count=1 ./tests/unit
ok      questionnaire/tests/unit        0.008s
node --test tests/frontend/frontend.test.cjs
✔ text answers survive forward/back navigation and JSON submission with ID **proto** (15.174429ms)
✔ number answers survive forward/back navigation and JSON submission with ID **proto** (2.785189ms)
✔ single answers survive forward/back navigation and JSON submission with ID **proto** (2.804461ms)
✔ multiple answers survive forward/back navigation and JSON submission with ID **proto** (3.923748ms)
✔ text answers survive forward/back navigation and JSON submission with ID constructor (2.415143ms)
✔ number answers survive forward/back navigation and JSON submission with ID constructor (2.719783ms)
✔ single answers survive forward/back navigation and JSON submission with ID constructor (2.604068ms)
✔ multiple answers survive forward/back navigation and JSON submission with ID constructor (3.284881ms)
✔ text answers survive forward/back navigation and JSON submission with ID toString (2.287701ms)
✔ number answers survive forward/back navigation and JSON submission with ID toString (2.384928ms)
✔ single answers survive forward/back navigation and JSON submission with ID toString (2.161796ms)
✔ multiple answers survive forward/back navigation and JSON submission with ID toString (3.422601ms)
✔ submitting blocks back navigation until the response resolves (2.441568ms)
✔ submitting blocks back navigation until the response rejects (2.185439ms)
✔ saving locks every editor control, preserves entered content and blocks concurrent requests (5.448099ms)
✔ save failure unlocks the editor and retains changes for retry (3.828259ms)
✔ publish failure unlocks the editor and retains changes for retry (6.189299ms)
✔ publishing keeps the entire editor locked between saving and publishing (3.850657ms)
✔ statistics page raw text and numbers independently, keeping complete totals, choices and averages (6.522618ms)
✔ statistics pagination refreshes returned summaries when new submissions arrive (3.835995ms)
✔ failed statistics pagination retains the current page and allows retry (3.579718ms)
✔ switching question filters during pagination refreshes the visible card and releases its controls (3.357254ms)
✔ statistics ignores a stale resolve after navigation (4.556429ms)
✔ statistics ignores a stale reject after navigation (2.600472ms)
✔ single-choice branching uses visible option labels while storing stable IDs (5.672447ms)
✔ multiple-choice contains_all selects labels but persists an array of IDs (6.778975ms)
✔ dashboard hides archived questionnaires until requested (4.70416ms)
✔ editor offers three question types and saves text/number as fill subtypes (4.73393ms)
✔ editing legacy draft uses the fill model without losing question IDs (5.227162ms)
✔ new text fill subtype renders and submits the right answer (7.922157ms)
✔ new number fill subtype renders and submits the right answer (2.46195ms)
ℹ tests 31
ℹ suites 0
ℹ pass 31
ℹ fail 0
ℹ cancelled 0
ℹ skipped 0
ℹ todo 0
ℹ duration_ms 206.597301
[+] up 1/1
 ✔ Container data-management-systems-lab1-mongodb-1 Healthy                                                                                                               6.4s
ok      questionnaire/tests/unit        1.071s  coverage: 33.2% of statements in ./internal/...
ok      questionnaire/tests/integration 22.789s coverage: 80.4% of statements in ./internal/...
go vet ./...
mkdir -p bin
go build -o bin/survey-lab ./cmd/server

~/WorkSpace/Data-Management-Systems-Lab1
❯ make e2e
[+] up 1/1
 ✔ Container data-management-systems-lab1-mongodb-1 Healthy                                                                                                               0.5s
RUN_E2E=1 go test -count=1 -race -v ./tests/e2e
=== RUN   TestBrowserAcceptance
    e2e_test.go:32: ▶ real browser: creation, validation, branching, anonymity, paging and closure
          ✔ register and design three types including both fill subtypes using the actual editor (3945.182768ms)
          ✔ require login and validate a complete anonymous response (3424.758454ms)
          ✔ forward branching skips required questions and duplicate submissions fail (2960.865766ms)
          ✔ special question IDs and stable paginated answers work in a real browser (1503.145408ms)
          ✔ the creator closes the survey and respondents see its closed state (362.90126ms)
          ✔ creator can reopen, archive and restore without losing answers (1573.978503ms)
        ✔ real browser: creation, validation, branching, anonymity, paging and closure (14215.678708ms)
        ℹ tests 7
        ℹ suites 0
        ℹ pass 7
        ℹ fail 0
        ℹ cancelled 0
        ℹ skipped 0
        ℹ todo 0
        ℹ duration_ms 14637.480687

--- PASS: TestBrowserAcceptance (14.81s)
PASS
ok      questionnaire/tests/e2e 15.827s
```
