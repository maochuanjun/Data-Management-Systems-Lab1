# HTTP API 说明

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

## 四、问卷生命周期管理

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| DELETE | /api/surveys/{id} | 删除无历史答卷的草稿 |
| POST | /api/surveys/{id}/archive | 归档（已发布问卷会被关闭） |
| POST | /api/surveys/{id}/restore | 恢复归档 |
| POST | /api/surveys/{id}/reopen | 重新开启已关闭问卷 |

reopen 可以提交空对象以沿用未过期的截止时间，或以 deadline:null 取消截止时间。

## 五、可复用题目与版本

这些接口均要求登录。题目作者可创建版本、恢复和修改共享名单；作者及被共享用户可读取题目和版本、选入自己的问卷。无读取权限返回 404。共享覆盖该题目的所有版本，被共享用户不能替作者修改版本或授权。

| 方法 | 路径 | 作用与返回 |
| --- | --- | --- |
| GET | /api/questions | 返回本人创建或获共享的题目数组，附 `latestVersion` |
| POST | /api/questions | 创建题目及 v1，返回 201 和含 `latestVersion` 的题目 |
| GET | /api/questions/{id} | 题目归属及最新实际保存的版本 |
| GET | /api/questions/{id}/versions | 按版本号升序返回全部版本 |
| POST | /api/questions/{id}/versions | 从指定父版本派生新版本，返回 201 |
| GET | /api/question-versions/{id} | 读取一个不可变版本 |
| POST | /api/questions/{id}/restore | 复制历史内容为新版本，返回 201 |
| PUT | /api/questions/{id}/sharing | 按用户名整体替换共享名单，要求 `sharingRevision` 匹配 |

创建题目的请求：

~~~json
{
  "question": {
    "title": "年龄",
    "type": "fill",
    "fillType": "number",
    "required": true,
    "options": [],
    "validation": {"min": 0, "max": 120, "integer": true}
  },
  "note": "初始年龄题"
}
~~~

`question` 使用问卷题目的内容结构，但不得携带非空跳转规则或来源引用；其内部 `id` 由服务端规范为 `template`。版本保存题型、标题、必答、选项和校验约束。`note` 最多 500 个字符。历史 `text/number` 输入在此接口规范为 `fill + fillType`。

创建版本请求为 `{"baseVersionId":"版本ID","question":{...},"note":"修改说明"}`。省略 `baseVersionId` 时以当前最新实际版本为父版本；指定任一历史版本即可形成分支。父版本属于其他题目时返回 400。响应含 `id`、`questionId`、`version`、`parentVersionId`、`question`、`note`、`createdAt`。版本号全题目递增，允许空号，不表示线性父子关系；关系以 `parentVersionId` 为准。

恢复请求为 `{"versionId":"历史版本ID","note":"恢复说明"}`。返回的新版本内容取自指定历史版本，`parentVersionId` 指向恢复时读到的最新版本，`restoredFromVersionId` 指向历史来源。旧版本及已引用它的问卷不会被覆盖。

共享请求为 `{"usernames":["bob","carol"],"sharingRevision":0}`，最多 100 名已注册用户；空数组撤销全部共享。`sharingRevision` 从读取题目的响应获取，新建题目为 0；它独立于题目内容的版本号。每次成功保存共享名单后递增，响应返回更新后的题目、版本号和规范化用户名。缺失、null 或非法版本号返回 400；旧版本保存返回 409，名单保持不变，客户端需刷新并核对权限后重新提交，不能自动重试旧名单。已存在但没有该字段的题目按版本 0 处理，首次成功保存时原子写入版本 1。

作者可读到 `sharedWith` 和 `sharedUsernames`，接收者看不到其他接收者名单。授权共享题目不等于授权访问作者的问卷或答卷。

### 在问卷中引用版本

读取版本后，将其 `question` 内容放入现有创建/保存问卷请求的 `questions`，分配问卷内唯一 `id`，添加 `sourceQuestionId` 和 `sourceVersionId`。两项来源必须成对提供，并对应相同题目和相同内容，否则返回 400。`rules` 可按当前问卷编排，来源题目的内容不能随意修改。

例如，版本的 ID 为 `version_1`、所属题目为 `question_1`，选入问卷后将局部 ID 改为 `age_first`，同时设置 `sourceQuestionId=question_1`、`sourceVersionId=version_1`。答卷仍以 `age_first` 为键，同一版本可在一份问卷中出现多次。

撤销共享后，已经保存的问卷快照仍可用于原问卷：保存时要求局部 ID、来源和内容均与旧快照一致，跳转规则可继续编排。新增引用及题目跨问卷统计仍需当前读取权限，原问卷的单卷统计继续可用。要在草稿内独立修改内容，可同时移除两项来源字段；此后该题不参加原可复用题目的跨问卷统计。旧问卷没有来源字段时继续作为独立题目使用，不自动建立历史关联。

## 六、个人题库

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | /api/question-banks | 列出本人的题库 |
| POST | /api/question-banks | 创建题库，返回 201，初始 `revision=1` |
| GET | /api/question-banks/{id} | 读取本人题库 |
| PUT | /api/question-banks/{id} | 更新名称、说明和固定版本列表，携带当前 `revision` |
| DELETE | /api/question-banks/{id}?revision=1 | 按当前修订号删除题库 |

创建示例：`{"name":"基本资料","description":"常用人口信息题","versionIds":["version_1"]}`。更新时提交完整的可编辑字段及 `revision`，成功后修订号加一，旧修订号返回 409。名称不能为空且最多 100 个字符，说明最多 2,000 个字符，最多固定 1,000 个不重复版本 ID。

题库固定的是具体版本；题目产生新版本不会自动更新题库或问卷。新增版本引用需要当前权限，原有固定版本可在共享撤销后保留，但不能借此绕过读取或新建问卷引用的权限。删除题库或移除固定项不会删除版本、问卷或答卷。题库本身为个人所有；共享发生在题目层。

## 七、使用情况与跨问卷统计

| 方法 | 路径 | 返回内容 |
| --- | --- | --- |
| GET | /api/questions/{id}/usage | 本人问卷引用明细及引用数量 |
| GET | /api/questions/{id}/stats | 各版本统计摘要，不返回原始答案 |
| GET | /api/questions/{id}/stats?versionId=版本ID&offset=0&limit=20 | 指定版本摘要及分页原始答案 |

使用情况含 `ownSurveyCount`、`totalSurveyCount`、`totalOccurrenceCount` 和 `surveys`。`surveys` 仅列当前用户自己的问卷；题目作者可查看全体用户的引用总量，被共享用户的数量也仅限自己的问卷。数量包括草稿、关闭及归档问卷的现存引用，不要求已经产生答卷。同一题目在一份问卷出现两次，问卷数计一、引用数计二。

**跨问卷统计始终只纳入当前登录用户创建的问卷。** 题目作者也不能读取其他用户引用该题后产生的回答。统计结果按不可变版本隔离；改动题型、选项或内容后的版本不混算。

响应结构：

~~~json
{
  "questionId": "question_1",
  "totalAnswers": 3,
  "surveyCount": 2,
  "versions": [{
    "versionId": "version_1", "version": 1,
    "title": "年龄", "type": "fill", "fillType": "number",
    "answered": 3, "surveyCount": 2, "options": [], "average": 20,
    "numericDistribution": [
      {"min": 10, "max": 20, "count": 1},
      {"min": 20, "max": 30, "count": 1},
      {"min": 30, "max": 30, "count": 1}
    ],
    "values": [10, 20], "valuesOffset": 0, "valuesLimit": 2, "hasMore": true
  }]
}
~~~

上例为指定版本并以 `limit=2` 查询三个数值 10、20、30 的示意结果。数值分布由 MongoDB 自适应分为最多 10 桶，`min` 包含下界，`max` 不包含上界，最后一桶同时包含上界；实际桶数由数据决定。

`totalAnswers`/`answered` 计回答的题目实例数，`surveyCount` 计实际含这些回答的不同问卷数。缺失、`null` 和被跳过的答案不计数；有效空文本、空数组和数字 0 沿用单卷统计口径。重复提交及同一版本在问卷中的多次出现都会分别计入，并非去重人数。数值平均值直接对所有有效数值计算，不对各问卷平均值再次求平均。

`offset` 为非负整数，`limit` 为 1–50、默认 20，重复分页参数返回 400。不指定版本时始终只返回摘要，`values=[]`、`valuesLimit=0`；指定版本后才返回原始答案页。原始答案按提交时间、答卷 ID、问卷 ID、局部题目 ID 稳定排序。并发新增答案时，多次请求不保证同一时点快照。
