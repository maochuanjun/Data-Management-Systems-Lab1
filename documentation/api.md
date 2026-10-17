# HTTP API 说明（第一阶段）

服务使用 Go 标准库 net/http，接口输入输出均为 JSON。返回错误时统一使用：

~~~json
{"error": "具体错误信息"}
~~~

常见状态码：**200** 成功，**201** 创建成功，**400** 参数或答案不合法，**401** 未登录，**404** 不存在或无访问权限，**409** 状态冲突、版本冲突或重复填写，**500** 数据库错误。

除注册和登录外，接口均要求登录会话 Cookie。路径中的 public 只表示填写者不必是问卷创建者，**不表示可以不登录**。

## 一、账号与问卷

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| POST | /api/register | 注册并自动登录 |
| POST | /api/login | 登录 |
| POST | /api/logout | 退出 |
| GET | /api/me | 查询当前用户 |
| GET | /api/surveys | 查看自己创建的问卷 |
| POST | /api/surveys | 创建问卷草稿 |
| GET | /api/surveys/{id} | 获取自己的一份问卷 |
| PUT | /api/surveys/{id} | 保存草稿（带 revision） |
| POST | /api/surveys/{id}/publish | 发布问卷 |
| POST | /api/surveys/{id}/close | 关闭问卷 |

注册或登录请求：

~~~json
{"username": "alice", "password": "password123"}
~~~

创建草稿至少应提供标题和问卷基本配置；也可以先用空 questions 数组创建，再逐步编辑。保存草稿时带上上一次读取到的 revision；保存成功后 revision 加一。发布前必须至少有一道合法题目，不能在截止时间已经过去的情况下发布。

一份简化的保存请求：

~~~json
{
  "revision": 1,
  "title": "课程反馈",
  "description": "问卷说明",
  "allowAnonymous": true,
  "allowRepeat": false,
  "deadline": null,
  "questions": [
    {
      "id": "q1",
      "title": "是否继续？",
      "type": "single",
      "required": true,
      "options": [
        {"id": "yes", "label": "是"},
        {"id": "no", "label": "否"}
      ],
      "validation": {"integer": false},
      "rules": [{"operator": "eq", "value": "no", "target": "END"}]
    },
    {
      "id": "q2",
      "title": "年龄",
      "type": "fill",
      "fillType": "number",
      "required": true,
      "options": [],
      "validation": {"min": 0, "max": 120, "integer": true},
      "rules": []
    }
  ]
}
~~~

**题型约定：** single 为单选，multiple 为多选，fill 为填空；fillType=text/number 表示文本/数字。多选答案是选项 ID 数组，minSelect/maxSelect 限制数量；文本填空使用 minLength/maxLength，数字填空使用 min/max/integer。为读取旧数据，后端仍接受历史 type=text/number。

## 二、填写与跳转

填写页面地址：**/survey/{id}**。

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | /api/public/surveys/{id} | 登录后获取已发布/已关闭问卷的可见内容 |
| POST | /api/public/surveys/{id}/next | 校验当前题答案，计算下一个题目 ID |
| POST | /api/public/surveys/{id}/responses | 提交整份答卷 |

下一题请求：

~~~json
{"questionId": "q1", "answer": "no"}
~~~

成功响应：

~~~json
{"answer": "no", "nextQuestionId": "END"}
~~~

提交示例（对应 q1 选择“是”，继续填写 q2）：

~~~json
{
  "anonymous": true,
  "answers": {"q1": "yes", "q2": 21}
}
~~~

成功提交返回 201，包含答卷 ID 和提交时间。服务器会重新计算真实作答路径，不接受被跳过题目的答案；违反题型约束返回 400；问卷关闭、截止时间已到或重复提交被禁止时返回 409。anonymous=true 仅在创建者允许匿名时有效。

## 三、统计（仅创建者）

| 方法 | 路径 | 结果 |
| --- | --- | --- |
| GET | /api/surveys/{id}/stats | 整卷汇总及每题第一页原始答案 |
| GET | /api/surveys/{id}/stats?summaryOnly=true | 整卷汇总，不加载原始答案 |
| GET | /api/surveys/{id}/stats?questionId=q2&offset=0&limit=20 | 某题统计及原始答案分页 |

返回字段包括 totalResponses、questions[].answered、options[].count、average、values、hasMore。数字填空计算 average；文本/数字内容在 values 中按页返回。limit 可设置 1 至 50，offset 为非负整数。

统计按**提交记录**计数，允许重复提交时并非去重人数。具体 MongoDB 聚合说明见 [数据库与核心逻辑设计](design.md)。

## 四、已有的辅助管理接口

以下已在代码中实现，但不是第一次大作业初始需求的必需项：

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| DELETE | /api/surveys/{id} | 删除无历史答卷的草稿 |
| POST | /api/surveys/{id}/archive | 归档（已发布问卷会被关闭） |
| POST | /api/surveys/{id}/restore | 恢复归档 |
| POST | /api/surveys/{id}/reopen | 重新开启已关闭问卷 |

reopen 可以提交空对象以沿用未过期的截止时间，或以 deadline:null 取消截止时间。
