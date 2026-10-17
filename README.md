# 基于 MongoDB 的在线问卷系统

《当代数据管理系统》第一次大作业 · 第一阶段

本项目实现一个简单的在线问卷系统：用户登录后创建、发布问卷，其他已登录用户通过链接填写，系统将答案存入 MongoDB，并提供整卷及单题统计。

课程重点是 **MongoDB 数据模型、后端校验、条件跳转和统计**。前端使用原生 HTML/CSS/JavaScript，只承担问卷编辑、填写和结果展示。

## 一、作业功能

| 功能 | 当前实现 |
| --- | --- |
| 账号与权限 | 注册、登录、退出；只能管理和统计自己的问卷 |
| 问卷管理 | 创建草稿、设置标题和说明、匿名选项、截止时间、发布和关闭、分享链接 |
| 三类题型 | 单选、多选、填空；填空分文本和数字两种子类型 |
| 答案约束 | 必答、选项有效性、多选数量、文本长度、数字范围及整数检查 |
| 条件跳转 | 单选、多选、数字填空按问卷中保存的规则跳转或提前结束 |
| 提交与统计 | 登录填写、匿名选项、允许/禁止重复提交、保存答卷、查看整卷及单题统计 |

问卷的题目和跳转规则内嵌在 MongoDB **surveys** 集合中；每次提交作为独立文档存入 **responses** 集合。数据库设计理由、索引及校验分工见 [MongoDB 设计说明](documentation/design.md)。

## 二、运行

需要 Go 1.26+、Docker（包含 Compose）和 Make。默认的 MongoDB 连接信息、数据库名、监听地址及端口均在根目录 **Makefile** 中定义，无需 Python 或 `.env` 文件。

~~~sh
make run
~~~

`make run` 会先通过 Docker Compose 启动并等待 MongoDB 健康检查通过，然后直接运行 Go 服务。浏览器访问 http://127.0.0.1:8080 。

- 默认 MongoDB 地址为 `127.0.0.1:27018`，数据库名为 `questionnaire_lab1`；容器由 `compose.yaml` 定义，数据存放在 Docker 命名卷中。
- 已有独立 MongoDB 时无需 Docker：用 `MONGO_URI='mongodb://127.0.0.1:27017' MONGO_DB=my_survey make run` 指定地址即可（自定义 URI 时不会自动启动 Compose）。
- Makefile 还支持 `LISTEN_ADDR`、`PORT`、`COOKIE_SECURE` 和仅用于测试的 `INTEGRATION_MONGO_URI` 参数。密码等敏感信息应通过本地环境变量传入，不要提交到仓库。
- 分享链接形如 `/survey/{id}`；填写者须先登录，匿名提交仅表示答卷不直接保存用户 ID。
- 跨设备访问时需要调整监听地址及网络配置；本项目未提供公网部署。

## 三、演示顺序

1. 注册账号 A，创建问卷，依次添加单选、多选、文本填空和数字填空，并配置答案限制。
2. 为单选、多选或数字填空设置跳转目标（后面的题目或 END），保存并发布。
3. 复制问卷链接，用账号 B 登录填写，演示命中跳转、非法答案提示及正确提交。
4. 用账号 A 查看各选项次数、回答份数、数字平均值及填空内容；再演示禁止重复提交。
5. 关闭问卷，检查新的提交被拒绝。

页面中还保留少量已实现的辅助管理操作；这些操作不是作业核心要求。

## 四、测试

~~~sh
make test          # Go 单元测试 + JavaScript 前端测试（不需要 MongoDB）
make integration   # 自动启动 MongoDB，执行真实数据库集成测试
make verify        # 核心测试 + 静态检查 + 构建（不含可选浏览器测试）
~~~

浏览器端到端测试是补充验收，首次执行需要安装 Node.js 开发依赖和 Chromium：

~~~sh
npm install
npx playwright install chromium
make e2e
~~~

`make integration` 默认会先启动并等待 Docker Compose 中的 MongoDB；指定不同的 `INTEGRATION_MONGO_URI` 时则连接已有数据库，不启动 Compose。集成测试创建和清理独立随机测试库，不会清空正常问卷数据。集成测试失败时不保留不完整的 `coverage.out`。

测试用例的**步骤、输入、预期输出、实际结果填写位置**见 [测试说明](documentation/testing.md)。仓库中原有的历史测试通过记录不等于这份最终代码已经复测；正式提交请运行测试并保存真实输出。

## 五、代码与提交材料

| 路径 | 内容 |
| --- | --- |
| cmd/server/ | 服务启动 |
| internal/domain/ | 三类题型、答案约束、跳转及整卷提交校验 |
| internal/store/ | MongoDB Schema、索引、CRUD 和聚合统计 |
| internal/web/ | HTTP 接口、会话与前端页面 |
| tests/ | 课程六类测试及补充回归 |
| [项目完成报告](项目完成报告.md) | 按课程评分重点整理的第一阶段报告 |
| [MongoDB 设计说明](documentation/design.md) | 集合、嵌入与引用、索引、统计、业务约束 |
| [API 说明](documentation/api.md) | 请求方式、参数及返回结果 |
| [测试说明](documentation/testing.md) | 六类用例与复测记录 |
| [AI 使用记录](documentation/ai-log.md) | 真实提示词摘录、代码产物、问题及修改 |

第二阶段的新需求尚未确定。现阶段保留既有数据和代码结构，不预先实现未经发布的第二阶段功能。
