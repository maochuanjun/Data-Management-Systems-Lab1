# HTTP API 说明（第一阶段）

服务使用 Go 标准库 net/http，接口输入输出均为 JSON。返回错误时统一使用：

~~~json
{"error": "具体错误信息"}
~~~

常见状态码：**200** 成功，**201** 创建成功，**400** 参数或答案不合法，**401** 未登录，**404** 不存在或无访问权限，**409** 状态冲突、版本冲突或重复填写，**413** 请求体超过接口允许的字节数，**500** 数据库错误。

注册、登录无需已有会话。退出接口是幂等操作：没有会话 Cookie 时也返回 200；其余业务接口要求有效的登录会话 Cookie。路径中的 public 只表示填写者不必是问卷创建者，仍需登录。

**请求体容量：** `POST /api/public/surveys/{id}/responses` 允许最多 **16 MiB（16,777,216 字节）** 的 JSON 请求体；其他接收 JSON 的接口上限为 **1 MiB（1,048,576 字节）**。超过相应上限返回 413。此限制按传输的请求体字节计算，与题目和答案的业务限制分别检查：问卷最多 100 道题，每个文本答案最多 10,000 个 Unicode 码点；题目配置还可以设置更小的长度上限。

## 一、账号与问卷

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| POST | /api/register | 注册并自动登录 |
| POST | /api/login | 登录 |
| POST | /api/logout | 退出；无会话时也返回 200 |
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

**题型约定：** single 为单选，multiple 为多选，fill 为填空；fillType=text/number 表示文本/数字。多选答案是选项 ID 数组，minSelect/maxSelect 限制数量；例如同时设置为 2，表示必须恰好选择两个不同选项。文本填空使用 minLength/maxLength，数字填空使用 min/max/integer。当前前端新建或编辑题目时统一使用 `fill + fillType`；服务端为兼容旧数据和旧调用方式，仍接受历史 `type=text/number` 的读取及新写入请求，未强制把这些请求转换为 `fill`。

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

成功提交返回 201，包含答卷 ID 和提交时间。服务器会重新计算真实作答路径，不接受被跳过题目的答案；违反题型约束返回 400；问卷关闭、截止时间已到或重复提交被禁止时返回 409。

`anonymous=true` 仅在问卷 `allowAnonymous=true` 时有效，此时答卷不写 `userId`；本次 `anonymous=false` 时仍写 `userId`，即使问卷允许匿名也是如此。`allowRepeat=false` 时，无论本次是否匿名，系统都按账号和问卷生成去重标记，阻止同一账号再次提交。匿名不表示免登录，也不保证数据库层面完全不可关联。

## 三、统计（仅创建者）

| 方法 | 路径 | 结果 |
| --- | --- | --- |
| GET | /api/surveys/{id}/stats | 整卷汇总及每题第一页原始答案 |
| GET | /api/surveys/{id}/stats?summaryOnly=true | 整卷汇总，不加载原始答案 |
| GET | /api/surveys/{id}/stats?questionId=q2&offset=0&limit=20 | 某题统计及原始答案分页 |

返回字段包括 totalResponses、questions[].answered、options[].count、average、values、hasMore。数字填空计算 average；文本/数字内容在 values 中按页返回。limit 可设置 1 至 50，offset 为非负整数。

`totalResponses` 是已保存的提交记录数。`answered` 统计该题答案字段存在且不为 `null` 的记录：允许提交的空字符串和空选项数组也计入，数字 0 作为正常数字计入回答数及平均值；字段缺失或为 `null` 时不计入。多选题的 `options[].count` 逐选项计数，空数组不会增加任一选项的次数。

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
