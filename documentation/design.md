# 系统设计与 MongoDB 数据模型（第一阶段）

## 一、系统结构

浏览器使用原生 JavaScript 调用 Go 提供的 JSON API。后端按职责分为三部分：

~~~text
网页（创建、填写、统计）
    ↓ HTTP / JSON
internal/web       身份认证、路由、所有权检查
    ↓
internal/domain    问卷校验、答案校验、条件跳转
    ↓
internal/store     MongoDB 读写、索引、聚合统计
    ↓
MongoDB
~~~

`cmd/server/main.go` 启动服务，`internal/store/mongo.go` 的 `collectionSchemas()` 定义数据库校验器。整个项目只使用 MongoDB 存储业务数据，不接入关系数据库或现成问卷系统框架。

## 二、集合设计

| 集合 | 主要字段 | 存储目的 |
| --- | --- | --- |
| `users` | `_id`、`username`、`passwordHash`、`createdAt` | 注册用户；密码保存 bcrypt 哈希 |
| `sessions` | `_id`、`userId`、`expiresAt` | 登录会话；令牌经 SHA-256 哈希后入库 |
| `surveys` | `_id`、`ownerId`、`title`、`status`、`questions`、`deadline`、`allowAnonymous`、`allowRepeat`、`revision`、`schemaVersion` | 问卷元数据、内嵌题目、选项和跳转规则 |
| `responses` | `_id`、`surveyId`、`anonymous`、`answers`、`path`、`submittedAt`、`schemaVersion` | 每次提交一份独立答卷 |

`surveys` 的 `questions` 数组内含题目类型、`required`、`options`、`validation` 和 `rules`。题目和选项使用稳定 ID，避免更改显示文字后影响规则引用。`archived` 用于归档；缺少此字段的旧问卷视为未归档。

一个简化的问卷片段如下：

~~~json
{
  "_id": "survey_001",
  "ownerId": "user_001",
  "schemaVersion": 1,
  "revision": 2,
  "title": "课程反馈",
  "status": "published",
  "allowAnonymous": true,
  "allowRepeat": false,
  "questions": [
    {
      "id": "q1",
      "title": "是否继续填写？",
      "type": "single",
      "required": true,
      "options": [
        {"id": "yes", "label": "是"},
        {"id": "no", "label": "否"}
      ],
      "validation": {"integer": false},
      "rules": [
        {"operator": "eq", "value": "no", "target": "END"}
      ]
    }
  ]
}
~~~

省略了问卷说明、创建时间、截止时间等字段。这是用于解释结构的示例，不是完整的数据库插入脚本。

每次提交单独存放在 `responses`，其中 `answers` 是以题目 ID 为键的答案对象，`path` 记录实际经过的题目 ID。例如选“否”并提前结束时，`answers={"q1":"no"}`、`path=["q1"]`。

## 三、为什么这样设计

- **嵌入题目**：题目、选项和规则通常随问卷整体读取与修改。放在同一个 `surveys` 文档中，可一次读取完整配置，减少关联查询。
- **分离答卷**：回答会不断增加。如果全部嵌入问卷文档，会造成文档无限增长，也不利于并发提交和统计；因此每次提交新增一条 `responses` 记录。
- **独立账户和会话**：`users` 负责身份，`sessions` 负责过期与退出，二者与问卷数据分开。
- **不用关系型拆表方案**：如果采用关系数据库，通常需要 `surveys/questions/options/rules/responses` 等多张表及关联查询。该应用主要读取整份问卷且题型答案结构不完全一致，MongoDB 嵌入文档更贴近这一访问模式。关系数据库在复杂跨问卷关联分析等场景也有优势；本课程明确要求使用 MongoDB。

## 四、索引与数据约束

| 集合 | 索引 | 用途 |
| --- | --- | --- |
| `users` | `username` 唯一 | 不允许同名注册 |
| `sessions` | `expiresAt` TTL | 自动删除过期会话 |
| `surveys` | `ownerId + createdAt` | 按创建者查询、排序 |
| `responses` | `surveyId + submittedAt + _id` | 统计查询与稳定分页 |
| `responses` | `surveyId + dedupeKey` 部分唯一 | 禁止同一账号重复提交时进行数据库级去重 |

初始化时，为四个集合建立 MongoDB JSON Schema 校验器，约束必需字段、基础 BSON 类型和状态、题型枚举。跨题目引用是否合法、规则目标是否向后、答案是否满足选择数量等，需要 Go 领域层校验，不能只依靠 MongoDB Schema。

## 五、条件跳转与提交校验

问卷草稿保存规则；发布时统一检查。支持：

| 题型 | 比较条件 |
| --- | --- |
| 单选 | `eq`：选中的选项 ID 相等 |
| 多选 | `contains`、`contains_all`：包含一个或全部选项 ID |
| 数字 | `eq`、`gt`、`gte`、`lt`、`lte` |

规则按数组顺序判断，第一条命中则跳到指定题目或 `END`；否则进入下一题。目标必须是后续题目或 `END`，第一阶段不支持循环。

服务端在最终提交时，从第一题重新推导真实路径，逐题检查必填、选项、数量、字数及数值约束；拒绝伪造的已跳过题目答案。前端校验主要提供即时提示，不替代服务端校验。

问卷状态为 `draft → published → closed`，允许从 `closed` 重新开启。已发布题目被冻结；草稿更新使用 `revision` 乐观锁，避免并发覆盖。归档不删除已有答卷。

## 六、统计与已知限制

`responses` 按 `surveyId` 聚合，通过 MongoDB `$facet` 为题目计算回答份数、选项次数和数字平均值。原始文本/数字答案单独分页读取，避免聚合形成超大 BSON 文档。统计接口支持整卷、单题、只读汇总以及按题目分页（详见 [API 说明](api.md)）。

现阶段统计按提交份数而非去重人数计算。匿名答卷本身不含 `userId`，但禁止重复提交时保存可由账号重算的去重哈希，因此不属于完全不可关联的匿名。问卷关闭和已经开始的并发提交也不构成严格原子事务；汇总与原文分页之间不保证同一事务快照。

为第二阶段保留 `schemaVersion`、稳定题目 ID、`revision` 和固定 v1 数据夹具；**当前没有实现通用自动迁移程序**，新需求发布后仍需明确设计迁移与兼容方式。
