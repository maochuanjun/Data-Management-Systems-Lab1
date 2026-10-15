# 系统与数据设计


## 结构

```text
浏览器（静态 HTML/CSS/原生 JS）
          │ JSON / HTTP，Cookie 会话
          ▼
internal/web        路由、认证、JSON 编解码、所有权检查
          ▼
internal/domain     问卷/答案校验、规则匹配、路径推导
          ▼
internal/store      MongoDB v2 实现、索引、聚合
          ▼
MongoDB             users / surveys / responses / sessions
```

`domain` 不依赖 HTTP 或 MongoDB。`web` 包定义 `Repository` 接口，处理器依赖该接口而不直接依赖 MongoDB，便于 HTTP 测试使用替身；生产启动时注入 `store.Store` 的 MongoDB 实现。`web` 只把经过认证与解码的数据交给领域层和存储层，不能相信浏览器给出的跳转路径、用户 ID 或统计结果。

## MongoDB 集合

| 集合 | 责任 | 主要字段与索引 |
| --- | --- | --- |
| `users` | 账户 | `_id`、`username`、`passwordHash`、`createdAt`；`username` 唯一索引 |
| `surveys` | 问卷及其嵌入题目 | `_id`、`ownerId`、`schemaVersion`、`revision`、状态与 `questions`；`ownerId,createdAt` 索引 |
| `responses` | 每次提交 | `_id`、`surveyId`、`schemaVersion`、可选 `userId`、`anonymous`、`dedupeKey`、`answers`、`path`、`submittedAt`；`surveyId,submittedAt` 索引；`surveyId,dedupeKey` partial unique 索引 |
| `sessions` | 服务端会话 | `_id` 为 token 的 SHA-256 哈希，`userId`、`expiresAt`；`expiresAt` TTL 索引 |

问卷和题目/选项/规则一同读取、共同发布，故嵌入 `surveys.questions`；一次完整回答也天然作为一个原子单元，故单独存为一个 `responses` 文档。账户、会话和可无限增长的回答没有嵌入到问卷或用户中，避免文档无限增长和无关访问。此方案利用 MongoDB 对嵌入文档、动态答案字段和单文档原子写入的适配性，而不将关系模型机械拆成大量“表”。

### Survey 形状（摘要）

```json
{
  "_id": "sv_...", "schemaVersion": 1, "revision": 3,
  "ownerId": "usr_...", "title": "产品反馈", "description": "...",
  "status": "draft", "allowAnonymous": true, "allowRepeat": false,
  "deadline": "2026-10-20T12:00:00Z", "createdAt": "...",
  "questions": [{
    "id": "q1", "title": "年级", "type": "single", "required": true,
    "options": [{"id":"o1","label":"大一"}, {"id":"o2","label":"大二"}],
    "validation": {"integer": false},
    "rules": [{"operator":"eq","value":"o1","target":"q3"}]
  }]
}
```

### Response 形状（摘要）

```json
{
  "_id": "rsp_...", "schemaVersion": 1, "surveyId": "sv_...",
  "anonymous": true,
  "dedupeKey": "sha256(surveyID:userID)",
  "answers": {"q1":"o1", "q3": 18}, "path": ["q1", "q3"],
  "submittedAt": "..."
}
```

匿名时省略 `userId`；非匿名回答包含 `userId`。`dedupeKey` 只在 `allowRepeat=false` 时写入，且仅用于唯一性约束。它是确定性的假名标识：拥有用户记录和数据库访问权的一方可以枚举用户 ID 并重算哈希以关联回答，因而它不提供管理员级别的不可关联性。partial unique 索引只约束含此字段的文档，从数据库层消除“先查询后插入”带来的竞争窗口。

## 校验、跳转和状态

1. 保存草稿与发布前执行 `ValidateSurvey`：标题、稳定 ID、题型约束、选项、数值范围以及只能向前跳转的规则均须合法。
2. 浏览器的“下一题”接口只用于交互预览；提交时 `ValidateSubmission` 从第一题重走规则，产生服务端 `path` 和归一化 `answers`。
3. 对经过路径的每题执行类型、必填和长度/数量/范围校验；有答案却不在路径中的题目一律拒绝。
4. 发布要求合法且非空的问卷；发布后题目和配置不可修改。推进题目与提交均检查 `status==published` 和当前时间早于 deadline；公开元数据读取可返回关闭或到期问卷供界面展示提示。关闭只允许 `published → closed`。
5. 草稿更新 MongoDB 过滤条件为 `_id + ownerId + status:draft + revision`，更新时 `$inc: {revision: 1}`；匹配数为零即 `409`。

## 认证与安全边界

密码只保存 bcrypt 哈希。登录成功后生成随机 token；浏览器只获得 token 原文的 `HttpOnly` cookie，数据库仅保存其 SHA-256 哈希，`Secure` 由 `COOKIE_SECURE` 控制，`SameSite=Lax`。会话查询也比较 `expiresAt`，不依赖 TTL 删除任务的时效性。

所有提交和编辑都根据会话取得用户，不接受请求体中的 owner/user ID。题目 ID 限制为 `[a-zA-Z0-9_-]{1,64}`，避免其作为 `answers.<id>` 聚合路径时引入字段路径歧义。统计聚合不投影 `userId` 或 `dedupeKey`。

## 统计策略

对问卷先执行一次 `$match`，再用一个 `$facet` 把同一响应流分到总数和每道题的管道。每题仅汇总已回答人数、选项次数与数字平均值，不把全部原始答案聚合进单个 BSON 文档。汇总结果大小随题目/选项数量增长，不随回答数无限增长。

原始答案通过独立查询分页读取，默认每题 20 条、最多 50 条，以 `submittedAt`、`_id` 升序保证时间相同的回答也有确定顺序。前端文本/数字统计支持翻页。单个 `$facet` 保证汇总项来自同一输入流；分页读取与汇总之间可能出现新提交，整个响应没有事务级快照保证。分页限制解决单文档容量故障，仍不等同于完成生产负载优化；大量题目或很深的 offset 仍需按负载进一步优化。

## 参考资料

- [Go `net/http` Cookie API](https://pkg.go.dev/net/http)
- [Go `crypto/rand` API](https://pkg.go.dev/crypto/rand)
- [bcrypt API](https://pkg.go.dev/golang.org/x/crypto/bcrypt)
- [MongoDB Go Driver v2](https://pkg.go.dev/go.mongodb.org/mongo-driver/v2/mongo)
- [MongoDB Partial Indexes](https://www.mongodb.com/docs/manual/core/index-partial/)
- [MongoDB JSON Schema Validation](https://www.mongodb.com/docs/manual/core/schema-validation/)
- [MongoDB Go Driver Aggregation](https://www.mongodb.com/docs/drivers/go/current/aggregation/)

## 扩展顺序

新增题型先扩充领域模型与校验、补充测试，再接入编辑器和填写页面。修改已发布题目时应增加独立问卷版本及答卷版本引用，保留旧版本；不能直接修改已有答卷所引用的题目。数据迁移应按 `schemaVersion` 编写显式迁移并测试旧数据，当前版本尚未提供自动迁移工具。
