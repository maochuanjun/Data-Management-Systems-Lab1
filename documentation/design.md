# 系统与 MongoDB 设计

本文说明第一阶段已经实现的模型和主要取舍；完整项目目标及测试结果见 [项目完成报告](../项目完成报告.md)。

## 1. 系统结构

```text
浏览器（HTML / CSS / 原生 JavaScript）
             |
       JSON API / Cookie
             |
 internal/web       HTTP 路由、认证、权限控制
             |
 internal/domain    题目校验、跳转规则、提交路径
             |
 internal/store     MongoDB 读写、索引、校验器、统计
             |
 MongoDB            users / sessions / surveys / responses
```

后端使用 Go 标准库 `net/http`。HTTP 层通过 `Repository` 接口调用存储层；领域层不依赖具体 HTTP 框架和 MongoDB 驱动，便于单独测试。前端页面通过 `go:embed` 由同一服务提供。

## 2. 集合及访问方式

| 集合 | 保存内容 | 主要索引及原因 |
| --- | --- | --- |
| `users` | 用户 ID、用户名、bcrypt 密码哈希、注册时间 | `username` 唯一索引，防止重复注册 |
| `sessions` | token 哈希、用户 ID、过期时间 | `expiresAt` TTL 索引；读取时也检查过期时间 |
| `surveys` | 问卷基本信息、状态、可选的 `archived` 标记、版本以及内嵌题目、选项和规则 | `(ownerId, createdAt)`，用于创建者列表 |
| `responses` | 每份回答的问卷 ID、作答路径、答案、提交时间、匿名标志 | `(surveyId, submittedAt, _id)`，用于统计和稳定分页；`(surveyId, dedupeKey)` 部分唯一索引，用于禁止重复提交 |

四个集合均设置了 MongoDB JSON Schema 校验器，要求主要字段存在且类型正确；问卷状态、题型限制为指定枚举。JSON Schema 只处理基础形状，题目间引用、选项是否存在和跳转是否合法仍由领域层检查。

### 问卷示例（简化）

```json
{
  "_id": "survey_001",
  "schemaVersion": 1,
  "revision": 2,
  "ownerId": "user_001",
  "title": "课程反馈",
  "status": "published",
  "allowAnonymous": true,
  "allowRepeat": false,
  "questions": [
    {
      "id": "q1",
      "title": "是否愿意继续填写？",
      "type": "single",
      "required": true,
      "options": [
        {"id": "yes", "label": "愿意"},
        {"id": "no", "label": "不愿意"}
      ],
      "validation": {"integer": false},
      "rules": [{"operator": "eq", "value": "no", "target": "END"}]
    }
  ]
}
```

### 回答示例（简化）

```json
{
  "_id": "response_001",
  "schemaVersion": 1,
  "surveyId": "survey_001",
  "anonymous": true,
  "answers": {"q1": "no"},
  "path": ["q1"],
  "submittedAt": "2026-10-15T08:00:00Z"
}
```

示例省略了部分字段，仅用于说明结构；实际 ID 由服务器生成。若问卷不允许重复，还会保存内部 `dedupeKey`。匿名答卷不保存 `userId`；非匿名答卷保存其 `userId`。

## 3. 为什么采用这样的 MongoDB 模型

题目、选项和跳转规则随问卷一起编辑、发布和读取，因此嵌入同一 `surveys` 文档：一次读取即可还原完整问卷，发布状态与题目配置不会出现跨文档不同步。回答数量会持续增长，所以将每次提交独立保存在 `responses`，避免把所有回答累积到一份问卷文档中。

如果使用关系数据库，常见做法是拆分 `surveys`、`questions`、`options`、`rules`、`responses`、`answer_items` 等表，通过外键和 JOIN 重建问卷。此项目的问卷结构层次明确、答案形态随题型变化，MongoDB 的嵌入文档更贴近主要读写场景。但这种选择并不意味着 MongoDB 总是优于关系数据库：复杂跨问卷分析、强跨文档事务和深分页仍有各自成本。

## 4. 关键业务规则

### 创建、发布和关闭

正常生命周期为 `draft → published → closed`，已关闭问卷允许 `closed → published`（重新开启），但不允许退回草稿。草稿允许编辑；发布时必须含有效题目并且截止时间未到。发布后题目、规则及配置冻结，重新开启也不会改变它们。

仅草稿允许永久删除。归档是与问卷状态相互独立的布尔标记 `archived`：默认列表隐藏归档项目，归档时若问卷为 `published` 则同时设为 `closed`；恢复归档不自动开启问卷。归档和恢复不删除任何回答。原有 v1 文档不含 `archived` 时视为 `false`，MongoDB JSON Schema 不要求历史文档补写字段。

重新开启时保留原答卷及重复提交约束，截止时间未过可复用；过期时要求设置新的未来时间或清除截止时间。归档状态、问卷状态、草稿更新均以 `revision` 做原子匹配，防止并发管理操作覆盖。保存草稿时按 `revision` 乐观锁更新，旧版本请求返回 `409`。

### 可配置跳转

跳转规则直接存放在题目的 `rules` 数组中。单选支持 `eq`；多选支持 `contains`、`contains_all`；数字支持 `eq/gt/gte/lt/lte`。从左到右依次匹配，第一条命中规则生效；未命中进入下一题。目标只能是后续题目或 `END`，当前版本不允许循环跳转。

### 提交校验

保存草稿、发布、下一题操作、最终提交都由服务端做相应校验。最终提交时调用领域层重新计算真实作答路径，只校验路径经过的题目，拒绝伪造额外答案。必答与多选数量、文本长度、数字范围和整数等限制均由服务器检查，不能依靠前端控件作为安全边界。

### 登录、匿名及重复提交

注册后密码只保存 bcrypt 哈希；会话 Cookie 设置 `HttpOnly` 和 `SameSite=Lax`，数据库保存随机 token 的 SHA-256 哈希。是否允许匿名和重复由问卷控制。

不允许重复时，`dedupeKey = SHA-256(surveyID + ":" + userID)`，写入 `responses` 的部分唯一索引确保并发情况下同一账号最多成功提交一次。该去重值为确定性假名标识，数据库管理员可以利用用户记录重算，故匿名只是不向业务统计界面暴露账号，而非不可追踪的强匿名。

## 5. 统计实现与取舍

`responses` 按 `surveyId` 过滤，再经 `$facet` 为各题计算回答份数、选项次数与数字平均值。单选/多选按稳定选项 ID 计数；数字通过 `$avg` 求均值。结果以**提交份数**为单位，允许重复提交时不代表独立人数。

为避免一次聚合把所有原始答案塞进 16 MiB 的单个 BSON 文档，原始答案使用独立分页查询，默认每页 20 条，最多 50 条，按 `(submittedAt, _id)` 稳定排序：

- `GET /api/surveys/{id}/stats`：兼容旧调用，返回汇总和每题的原始答案分页。
- `GET /api/surveys/{id}/stats?summaryOnly=true`：只运行汇总聚合，不逐题查询原始答案；网页统计概览使用此模式。
- `GET /api/surveys/{id}/stats?questionId=q1&offset=0&limit=20`：查看某题原始答案，按需加载并翻页。

`summaryOnly=true` 时每题 `values=[]`、`valuesLimit=0`，`hasMore` 表示该题是否存在可取回的答案。汇总与原始分页不在同一事务快照中，并发提交可能使两次读取的结果略有差异。旧版整卷统计路径仍可能逐题查询原始答案，应在高负载部署时限制使用或继续优化。

## 6. 第二阶段的增量修改基础

问卷和答卷带 `schemaVersion`；题目与选项以稳定 ID 引用，避免重排时错配原有答案；固定的 `tests/fixtures/v1` 用于旧数据回归。第二阶段必须保留现有核心功能，通过新增字段、版本化读取或显式迁移处理需求变化。当前**尚未实现通用自动迁移程序**，仅有版本标识与兼容性测试。

如果需要编辑已发布问卷，应优先设计发布版本和回答对应的版本号，而不是直接覆盖历史题目；已保存答卷不应被新配置重新解释。

## 7. 当前限制

- 关闭操作和已通过准入检查的并发提交不是一个严格原子事务；关闭后仍可能完成先前已获准的写入。
- 默认服务只监听本机；通过网络分发链接需要部署在可访问的主机上。
- 大量题目及历史统计接口仍有聚合和查询成本；深 `offset` 分页不适合无限增长的数据量。
- 当前题型与规则按第一阶段范围实现，不支持循环跳转或题目版本历史。单选和多选规则在编辑器按选项文字选择，MongoDB 中仍存储稳定选项 ID，修改显示文字不改变原有引用。

具体接口见 [API 文档](api.md)，验证情况见 [测试记录](testing.md)。
