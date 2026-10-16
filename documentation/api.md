# HTTP API 说明

所有接口由 Go `net/http` 实现，数据格式为 JSON。除注册、登录外，访问业务接口需要会话 Cookie。`/api/public/` 表示填写者无须拥有问卷，不表示免登录。

错误统一返回 `{"error":"错误说明"}`。主要状态码：`200` 操作成功，`201` 创建成功，`400` 参数或答案不合法，`401` 未登录，`404` 资源不存在或无权访问，`409` 状态冲突或重复提交，`500` 服务端错误。

## 一、账号

| 方法 | 路径 | 请求 / 功能 |
| --- | --- | --- |
| POST | `/api/register` | `{"username":"alice","password":"..."}`，注册并登录，返回 `201` |
| POST | `/api/login` | 同上，登录并设置会话 Cookie |
| POST | `/api/logout` | 退出，删除会话 |
| GET | `/api/me` | 查询当前用户 |

用户名为 3–32 位字母、数字或下划线。注册密码至少 8 个 Unicode 字符，且最多 72 个 UTF-8 字节。

## 二、问卷管理（仅创建者）

| 方法 | 路径 | 功能 |
| --- | --- | --- |
| GET | `/api/surveys` | 查看自己创建的问卷 |
| POST | `/api/surveys` | 创建草稿；请求体为问卷内容 |
| GET | `/api/surveys/{id}` | 查看自己的问卷 |
| PUT | `/api/surveys/{id}` | 保存草稿，需要提交当前 `revision` |
| POST | `/api/surveys/{id}/publish` | 发布非空、合法的草稿 |
| POST | `/api/surveys/{id}/close` | 关闭已发布问卷 |
| DELETE | `/api/surveys/{id}` | 永久删除草稿，不允许删除已发布问卷 |
| POST | `/api/surveys/{id}/archive` | 归档问卷；已发布则同时关闭 |
| POST | `/api/surveys/{id}/restore` | 恢复归档，不自动重新开启 |
| POST | `/api/surveys/{id}/reopen` | 重新开启已关闭、未归档的问卷 |
| GET | `/api/surveys/{id}/stats` | 查看问卷统计 |

创建或保存问卷时，主要可编辑字段有 `title`、`description`、`allowAnonymous`、`allowRepeat`、`deadline` 和 `questions`。保存草稿的示例：

~~~json
{
  "revision": 1,
  "title": "课程反馈",
  "description": "匿名反馈",
  "allowAnonymous": true,
  "allowRepeat": false,
  "deadline": null,
  "questions": [
    {
      "id": "q1",
      "title": "是否满意？",
      "type": "single",
      "required": true,
      "options": [
        {"id": "yes", "label": "满意"},
        {"id": "no", "label": "不满意"}
      ],
      "validation": {"integer": false},
      "rules": [{"operator": "eq", "value": "no", "target": "END"}]
    }
  ]
}
~~~

`PUT` 保存成功会将 `revision` 加一；旧版本请求返回 `409`。`reopen` 的请求体为 `{}`（保持原截止时间）、`{"deadline":null}`（取消截止时间）或 `{"deadline":"2026-11-01T12:00:00Z"}`（新的未来时间）。截止时间已经过去时不可保持原值直接重新开启。

## 三、填写问卷（需登录）

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/public/surveys/{id}` | 获取可访问问卷的公开内容 |
| POST | `/api/public/surveys/{id}/next` | `{"questionId":"q1","answer":"yes"}`，校验并返回下一题 ID |
| POST | `/api/public/surveys/{id}/responses` | `{"anonymous":true,"answers":{"q1":"yes"}}`，提交答卷，成功返回 `201` |

`next` 返回 `{"answer":"yes","nextQuestionId":"END"}` 等数据。实际提交由后端重新计算完整跳转路径；必答未填、选择数量不符、数字越界或包含已跳过题目答案时返回 `400`。关闭、到期或归档问卷不可再提交。不允许重复时，同一登录账号再次提交返回 `409`。

## 四、统计

- `GET /api/surveys/{id}/stats`：整卷汇总及每题第一页原始答案。
- `GET /api/surveys/{id}/stats?summaryOnly=true`：只返回计数和平均值，原始答案列表为空。
- `GET /api/surveys/{id}/stats?questionId=q1&offset=0&limit=20`：指定题目的统计和原始答案分页。

示例响应（省略其他题目）：

~~~json
{
  "totalResponses": 2,
  "questions": [{
    "id": "q1",
    "title": "是否满意？",
    "type": "single",
    "answered": 2,
    "options": [
      {"id": "yes", "label": "满意", "count": 1},
      {"id": "no", "label": "不满意", "count": 1}
    ],
    "values": ["yes", "no"],
    "average": null,
    "valuesOffset": 0,
    "valuesLimit": 20,
    "hasMore": false
  }]
}
~~~

`totalResponses`、`answered` 为提交记录数/有实际答案的提交份数，不是独立人数。数字题计算 `average`。`offset` 为非负整数，`limit` 为 1–50；原始答案按 `submittedAt + _id` 排序，`hasMore` 指示是否存在下一页。统计数据不返回填写者身份。
