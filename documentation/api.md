# HTTP API


基础路径为当前站点根路径，内容类型为 `application/json; charset=utf-8`。除登录、注册和静态资源外，接口用 `HttpOnly` 会话 Cookie 认证。错误响应统一为 `{"error":"面向用户的说明"}`。

## 状态码

| 状态 | 含义 |
| --- | --- |
| 200 | 查询、更新或状态操作成功 |
| 201 | 创建账户、问卷或回答成功 |
| 400 | JSON 格式、字段、题目/答案校验或截止时间不合法 |
| 401 | 未登录、会话无效或已过期 |
| 403 | 跨站请求被拒绝 |
| 404 | 资源不存在；或访问不属于自己的私有问卷/统计（不泄露所有权） |
| 409 | 用户名或唯一键重复、草稿 revision 冲突、重复提交、问卷状态迁移不允许 |
| 500 | 未预期的数据库或服务端错误 |

## 认证

| 方法与路径 | 请求体 | 成功响应 |
| --- | --- | --- |
| `POST /api/register` | `{"username":"alice","password":"..."}` | `201`，`{"id","username","createdAt"}` 并写入会话 Cookie |
| `POST /api/login` | 同上 | `200`，当前用户并写入会话 Cookie |
| `POST /api/logout` | 无 | `200`，`{"ok":true}`；删除服务端会话并使 Cookie 过期 |
| `GET /api/me` | 无 | `200`，当前用户；无会话为 `401` |

注册密码至少为 8 个 Unicode 字符、最多为 72 个 UTF-8 字节（bcrypt 输入上限）；用户名冲突返回 `409`。密码不出现在正常的响应或日志中。

## 创建者问卷接口

这些接口均要求登录，并且除列表/创建外均要求请求用户是该问卷 owner。

| 方法与路径 | 用途 | 成功响应 |
| --- | --- | --- |
| `GET /api/surveys` | 我的问卷列表 | `200`，Survey 数组，按创建时间倒序 |
| `POST /api/surveys` | 创建空草稿 | `201`，新 Survey |
| `GET /api/surveys/{id}` | 获取自己的问卷 | `200`，完整 Survey |
| `PUT /api/surveys/{id}` | 覆盖保存草稿 | `200`，revision 加一后的 Survey |
| `POST /api/surveys/{id}/publish` | 发布合法且非空草稿 | `200`，已发布 Survey |
| `POST /api/surveys/{id}/close` | 关闭已发布问卷 | `200`，已关闭 Survey |
| `GET /api/surveys/{id}/stats` | 全问卷统计（兼容旧查询） | `200`，Stats，包含各题第一页面原始答案 |
| `GET /api/surveys/{id}/stats?summaryOnly=true` | 仅取汇总，不读取原始答案 | `200`，全部题目的计数、均值和空的 `values` |
| `GET /api/surveys/{id}/stats?questionId=q1` | 单题统计 | `200`，只含 `q1` 的 Stats；未知题目为 `404` |
| `GET /api/surveys/{id}/stats?questionId=q1&offset=20&limit=20` | 原始答案下一页 | `200`，完整汇总和指定页原始答案 |

`PUT` 请求保存草稿的完整可编辑内容，特别是 `revision`；遗漏的可编辑字段会使用其零值。服务器保留原有 `id`、`ownerId`、`createdAt`，且拒绝修改已发布问卷。草稿示例：

```json
{
  "revision": 2, "title": "产品反馈", "description": "请如实填写",
  "allowAnonymous": true, "allowRepeat": false, "deadline": null,
  "questions": [{"id":"q1","title":"是否满意？","type":"single","required":true,
    "options":[{"id":"yes","label":"满意"},{"id":"no","label":"不满意"}],
    "validation":{"integer":false},"rules":[]}]
}
```

## 填写接口

路径中使用 `public` 表示不要求 owner，而非不要求登录。任何填写者仍需登录。草稿读取为 `404`；`next` 和 `responses` 仅接受 `published` 且未到期的问卷，关闭或过期时返回 `409`。

| 方法与路径 | 请求体 | 成功响应 |
| --- | --- | --- |
| `GET /api/public/surveys/{id}` | 无 | `200`，不含 owner 私有信息的 Survey；客户端据状态和 deadline 展示提示 |
| `POST /api/public/surveys/{id}/next` | `{"questionId":"q1","answer":"yes"}` | `200`，`{"answer":"yes","nextQuestionId":"q2"}`；服务器归一化答案 |
| `POST /api/public/surveys/{id}/responses` | `{"anonymous":true,"answers":{"q1":"yes","q2":18}}` | `201`，`{"id":"rsp_...","submittedAt":"..."}` |

`next` 只处理当前题答案并执行同一套单题校验；最终 `responses` 会重算全部路径，不能以多次 `next` 的结果代替最终校验。匿名标志为真但问卷不允许匿名时返回 `400`；关闭重复提交时再次提交为 `409`。

## Stats 响应

```json
{
  "totalResponses": 2,
  "questions": [{
    "id":"q1", "title":"是否满意？", "type":"single", "answered":2,
    "options":[{"id":"yes","label":"满意","count":1},{"id":"no","label":"不满意","count":1}],
    "values":["yes","no"], "average":null,
    "valuesOffset":0, "valuesLimit":20, "hasMore":false
  }]
}
```

`answered` 是实际经过该题且保存了非 null 答案的提交份数；被跳过题目和未提交答案的可选题不计入。明确提交的空字符串、空多选数组在通过校验后会计入该题回答数，但不会产生选项选择次数。数字题 `average` 为数值或 `null`，文本/选择题为 `null`。响应中不含身份或去重字段。

`values` 为分页原始答案，默认 `offset=0&limit=20`，按 `submittedAt`、`_id` 升序排列。`offset` 必须为非负整数，`limit` 为 1–50 的整数；无效、空值或重复分页参数返回 `400`。`valuesOffset`、`valuesLimit` 表示当前页，`hasMore` 表示仍有下一页；超出末页返回空数组。`totalResponses`、`answered`、选项计数与 `average` 始终是完整汇总，不受分页影响。全问卷查询对每题应用相同分页参数，通常应通过 `questionId` 单独翻页。

新增 `summaryOnly=true` 仅执行汇总，不做逐题的原始答案查询；此时每题 `values=[]`、`valuesLimit=0`，`hasMore` 表示是否有可按需获取的原始答案。参数只接受单个 `true` 或 `false`，重复或其他值返回 `400`；默认仍为 `false`，以保留旧客户端的分页行为。网页先用汇总模式展示数据，用户点击“查看填写内容”后才请求某题的分页数据。

汇总来自同一聚合输入流，原始答案另行分页查询；并发提交可能在两次读取之间出现，因此整个响应不保证事务级快照。客户端若需要全部原始答案，须根据 `hasMore` 继续翻页。整卷旧接口在题目很多时仍会执行逐题原始答案查询，建议使用汇总模式结合按题分页。
