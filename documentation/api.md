# HTTP API 说明

服务使用 Go 标准库 `net/http`，以 JSON 交换数据。接口按账号、问卷配置、可复用题目、个人题库、填写和统计组织；第一阶段的填写、跳转和单卷统计接口继续使用，第二阶段在这些接口旁增加题目来源与版本管理。

## 一、通用约定

### 1. 会话、请求与错误

注册和登录无需已有会话。退出是幂等操作，没有会话 Cookie 时也返回 200；其余业务接口要求有效的 `session` Cookie。路径中的 `public` 表示填写者不必是问卷创建者，仍需登录。会话 Cookie 使用 `HttpOnly` 和 `SameSite=Lax`，是否启用 `Secure` 由部署配置决定。

接收 JSON 的接口要求 `Content-Type: application/json`，可以附带字符集参数。请求只能包含一个符合接口字段定义的 JSON 对象；未知字段、无效 JSON 或业务参数不合法时返回 400。API 响应使用 `Cache-Control: no-store`。浏览器写请求必须通过同站检查，不允许的跨站请求返回 403。

错误响应统一为：

~~~json
{"error": "具体错误信息"}
~~~

| 状态码 | 含义 |
| --- | --- |
| 200 | 查询、更新、删除或状态操作成功 |
| 201 | 创建账号、问卷、版本、题库或答卷成功 |
| 400 | 请求格式、参数、题目配置或答案不合法 |
| 401 | 未登录、会话失效或登录凭据不正确 |
| 403 | 写请求未通过跨站检查 |
| 404 | 资源不存在，或当前用户没有对应访问权限 |
| 409 | 当前状态不允许操作、修订号冲突、账号已存在或禁止重复提交 |
| 413 | 请求体超过接口允许的字节数 |
| 500 | 数据库或服务器处理错误 |

### 2. 容量与修订号

`POST /api/public/surveys/{id}/responses` 最多接收 **16 MiB（16,777,216 字节）** 的 JSON 请求体；其他接收 JSON 的接口上限为 **1 MiB（1,048,576 字节）**。超限返回 413。这是请求体字节数限制，与业务限制分别检查：问卷最多 100 道题，每个文本答案最多 10,000 个 Unicode 码点；题目可以设置更小的长度上限。

问卷和题库的 `revision` 用于防止旧页面覆盖新修改；题目共享名单使用独立的 `sharingRevision`。它们与不可变内容的 `version` 不同。更新成功后，客户端应以响应中的最新修订号替换本地值；遇到 409 时重新读取并核对内容后再操作。

## 二、账号

| 方法 | 路径 | 成功响应 |
| --- | --- | --- |
| POST | /api/register | 201，用户对象，并建立会话 |
| POST | /api/login | 200，用户对象，并建立会话 |
| POST | /api/logout | 200，`{"ok":true}`，清除会话 |
| GET | /api/me | 200，当前用户对象 |

注册或登录请求：

~~~json
{"username": "alice", "password": "password123"}
~~~

注册用户名去除首尾空白后需为 3–32 位英文字母、数字或下划线；密码至少 8 个 Unicode 码点且不超过 72 个 UTF-8 字节。用户名重复返回 409，登录凭据错误返回 401。用户响应不包含密码散列。

## 三、问卷配置与生命周期

### 1. 读取、创建和保存

以下接口只操作当前用户创建的问卷，其他用户访问返回 404。

| 方法 | 路径 | 作用与成功响应 |
| --- | --- | --- |
| GET | /api/surveys | 200，本人问卷数组 |
| POST | /api/surveys | 201，新建草稿对象，初始 `revision=1` |
| GET | /api/surveys/{id} | 200，本人问卷对象 |
| PUT | /api/surveys/{id} | 200，保存后的草稿对象，`revision` 加一 |

创建时提供标题和基本配置，可先使用空 `questions` 数组。服务端设置 ID、所有者、创建时间和初始状态，客户端不能据此伪造他人问卷。保存仅适用于未归档的草稿，请求需包含上次读取的 `revision`；状态已变或修订号过期返回 409。

简化的保存请求：

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

### 2. 题型、选项和来源

`single` 为单选，`multiple` 为多选，`fill` 为填空，`fillType=text/number` 区分文本和数字。多选答案为选项 ID 数组，`minSelect/maxSelect` 限制选择数量；二者同时为 2 时，必须恰好选择两个不同选项。文本使用 `minLength/maxLength`，数字使用 `min/max/integer`。

前端新建或编辑题目统一使用 `fill + fillType`；服务端仍接受历史 `type=text/number` 的读取与问卷写入请求，不强制改写旧问卷。可复用题目接口会把这两种历史输入规范为 `fill + fillType`。

单选和多选的新建、编辑、发布配置要求选项 ID 唯一，显示文字去除首尾 Unicode 空白后也不能重复。比较区分大小写，不合并近似字符，不自动改写原始标签。重复文字返回 400，例如“第 2 题：第 2 个选项与第 1 个选项的文本重复”。新建可复用题目和编辑派生版本采用同一规则。

历史已发布问卷继续按稳定选项 ID 校验答案，原快照不改写；恢复历史版本也忠实复制原内容。含重复标签的历史内容若用于新的草稿，保存或发布前须先派生修正版，或转为独立题目修改。

从可复用版本选入问卷时，题目保留完整内容，另带成对的 `sourceQuestionId` 和 `sourceVersionId`。具体引用规则见第四节，答卷仍使用问卷内的局部题目 ID。

### 3. 发布、关闭、归档、恢复和删除

| 方法 | 路径 | 条件与结果 |
| --- | --- | --- |
| POST | /api/surveys/{id}/publish | 未归档的草稿发布；200，更新后的问卷 |
| POST | /api/surveys/{id}/close | 已发布问卷关闭；200，更新后的问卷 |
| POST | /api/surveys/{id}/archive | 未归档问卷归档；若已发布，同时关闭；200，更新后的问卷 |
| POST | /api/surveys/{id}/restore | 已归档问卷解除归档，保持原状态；200，更新后的问卷 |
| POST | /api/surveys/{id}/reopen | 已关闭且未归档的问卷重新开启；200，更新后的问卷 |
| DELETE | /api/surveys/{id} | 删除没有历史答卷的草稿；200，`{"ok":true}` |

状态条件不满足或并发状态变化返回 409。发布还要求至少一道合法题目、配置通过当前校验，且截止时间尚未过去；不符合这些配置条件返回 400。已发布问卷不能通过保存接口修改题目。解除归档不会自动恢复填写，已关闭的问卷需要另外执行重新开启。

`reopen` 接收 JSON 对象。`{}` 沿用原截止时间；`{"deadline":null}` 清除截止时间；也可以提供 RFC 3339 时间字符串，例如 `{"deadline":"2027-01-01T00:00:00Z"}`。最终采用的非空截止时间必须晚于操作时间，否则返回 400。省略 `deadline` 与显式 `null` 的含义不同。

## 四、可复用题目、版本与共享

### 1. 权限与接口

题目作者可创建版本、恢复和修改共享名单；作者及被共享用户可读取题目与版本，并选入自己的问卷。共享覆盖该题目的所有版本，被共享用户不能替作者修改内容或授权。无对应权限返回 404。共享题目不授予访问作者问卷、答卷的权限。

| 方法 | 路径 | 作用与成功响应 |
| --- | --- | --- |
| GET | /api/questions | 200，本人创建或获共享的题目数组，附 `latestVersion` |
| POST | /api/questions | 201，新题目及 v1，响应附 `latestVersion` |
| GET | /api/questions/{id} | 200，题目归属、共享修订号及最新实际保存的版本 |
| GET | /api/questions/{id}/versions | 200，按版本号升序排列的版本数组 |
| POST | /api/questions/{id}/versions | 201，从指定父版本派生的新版本 |
| GET | /api/question-versions/{id} | 200，一个不可变版本 |
| POST | /api/questions/{id}/restore | 201，复制历史内容形成的新版本 |
| PUT | /api/questions/{id}/sharing | 200，整体替换共享名单后的题目对象 |

### 2. 创建、修改和恢复

创建题目示例：

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

`question` 使用问卷题目的内容结构，但不能携带非空跳转规则或来源引用，内部 `id` 由服务端规范为 `template`。版本保存题型、标题、必答、选项和校验约束；跳转规则由使用该题的问卷编排。`note` 最多 500 个 Unicode 码点。

创建版本请求：

~~~json
{"baseVersionId":"历史版本ID","question":{},"note":"修改说明"}
~~~

其中 `question` 应替换为完整、合法的题目内容。省略 `baseVersionId` 时，以当前最新实际保存的版本为父版本；指定任一历史版本可以形成分支。父版本属于其他题目时返回 400，不存在时返回 404。

版本响应含 `id`、`questionId`、`version`、`parentVersionId`、`question`、`note`、`createdAt`。版本号在同一题目内递增，允许空号；它只表示编号顺序，父子关系以 `parentVersionId` 为准。新版本不会覆盖旧版本，也不会替换问卷中已经保存的快照。

恢复请求：

~~~json
{"versionId":"历史版本ID","note":"恢复说明"}
~~~

返回的新版本内容取自指定历史版本，`parentVersionId` 指向恢复时读取到的最新版本，`restoredFromVersionId` 指向恢复来源。来源属于其他题目时返回 400。恢复直接复制历史内容，不把历史快照按新配置规则改写；该版本用于新的草稿时，仍须满足当前保存和发布校验。

### 3. 共享名单

共享请求：

~~~json
{"usernames":["bob","carol"],"sharingRevision":0}
~~~

`usernames` 最多包含 100 项，必须对应已注册用户；服务端去除用户名首尾空白、合并重复用户，并排除作者本人。空数组撤销全部共享。用户名格式不合法返回 400，用户不存在返回 404。

`sharingRevision` 从题目响应获取，新建题目为 0。缺失、`null`、负数或非整数返回 400；旧值保存返回 409，原名单保持不变。成功后修订号加一，响应返回更新后的题目对象、`sharingRevision` 和规范化的 `sharedUsernames`。旧数据没有该字段时按 0 处理，首次成功保存原子写入 1。

客户端遇到共享冲突应刷新并核对名单，不能直接用旧名单自动重试。作者可以读取 `sharedWith` 和 `sharedUsernames`，接收者看不到其他接收者名单。

### 4. 在问卷中引用版本

读取版本后，将其 `question` 复制到创建或保存问卷请求的 `questions`，分配问卷内唯一的 `id`，再附上 `sourceQuestionId` 和 `sourceVersionId`。两项来源必须成对存在，对应同一题目，且快照内容与所选版本一致；不符合时返回 400。新增引用还需当前读取权限，否则返回 404。局部 `id` 和 `rules` 可按当前问卷安排，其他来源内容不能随意修改。

例如，版本 ID 为 `version_1`、所属题目 ID 为 `question_1`，选入问卷后使用局部 ID `age_first`，并设置 `sourceQuestionId=question_1`、`sourceVersionId=version_1`。答卷继续以 `age_first` 为键。同一版本在同一问卷中可以出现多次，每次使用不同的局部 ID。

共享撤销后，已保存的问卷继续使用原快照。后续保存时，局部 ID、来源和内容均与旧快照一致的题目可保留，跳转规则可继续编排；草稿保存和发布仍须通过当前配置校验。新增引用、重新读取题目及跨问卷统计继续要求当前读取权限，旧已发布问卷的填写与单卷统计不受影响。

若需要把草稿中的引用题目改成独立题目，应同时移除两项来源字段后修改；此后该题不再参加原可复用题目的跨问卷统计。没有来源字段的旧问卷继续作为独立题目使用，系统不按题面自动补造历史关联。

## 五、个人题库

| 方法 | 路径 | 作用与成功响应 |
| --- | --- | --- |
| GET | /api/question-banks | 200，本人题库数组 |
| POST | /api/question-banks | 201，新题库，初始 `revision=1` |
| GET | /api/question-banks/{id} | 200，本人题库 |
| PUT | /api/question-banks/{id} | 200，更新后的题库，`revision` 加一 |
| DELETE | /api/question-banks/{id}?revision=1 | 200，`{"ok":true}` |

创建示例：

~~~json
{"name":"基本资料","description":"常用人口信息题","versionIds":["version_1"]}
~~~

更新时提交完整的可编辑字段及当前 `revision`；删除时通过查询参数携带当前修订号。更新或删除缺失、非法或非正修订号返回 400，旧修订号返回 409。名称不能为空且最多 100 个 Unicode 码点，说明最多 2,000 个 Unicode 码点，最多固定 1,000 个不重复版本 ID。其他用户的题库不可读写，返回 404。

题库固定具体版本。题目产生新版本后，要显式调整 `versionIds` 才会更新题库；已有问卷也不会自动跟随。新增固定版本须存在且当前可读，已有固定项在共享撤销后可以保留，但不能据此绕过读取或新增问卷引用权限。删除题库、移除固定项不会删除版本、问卷或答卷。题库本身为个人所有，共享发生在题目层。

## 六、填写、校验与提交

填写页面为 `/survey/{id}`。下列接口要求填写者登录，但不要求其为问卷作者。

| 方法 | 路径 | 作用与成功响应 |
| --- | --- | --- |
| GET | /api/public/surveys/{id} | 200，已发布或已关闭问卷的可见内容，以及 `submissionBlocked` |
| POST | /api/public/surveys/{id}/next | 200，规范化答案及下一题 ID |
| POST | /api/public/surveys/{id}/responses | 201，答卷 ID 和提交时间 |

草稿和归档问卷不能通过公开读取接口访问，返回 404。读取已关闭问卷是为了展示状态，不表示可以提交。响应不暴露所有者信息、答卷或去重键。

### 1. 进入填写页前的重复检查

`submissionBlocked` 仅在 `allowRepeat=false` 且当前账号已有该问卷的去重标记时为 `true`，前端据此在首题前提示已经提交。允许重复时为 `false`。该字段不返回提交内容、时间或用户 ID；`false` 不保证当前开放，仍需检查状态和截止时间。查询失败返回错误，不把数据库异常当作“尚未提交”。

预检与最终写入不是同一个原子操作。另一标签页可能在预检后先提交，最终仍由唯一索引和 409 响应防止重复。匿名与实名使用相同的账号级标记；启动时对可识别账号的历史记录进行去重标记回填。

### 2. 下一题与整卷提交

下一题请求与响应：

~~~json
{"questionId":"q1","answer":"no"}
~~~

~~~json
{"answer":"no","nextQuestionId":"END"}
~~~

提交示例，对应第三节问卷中 q1 选择“是”并继续填写 q2：

~~~json
{"anonymous":true,"answers":{"q1":"yes","q2":21}}
~~~

服务器根据题目配置重新校验答案并计算真实路径，不接受未知题目或被跳过题目的答案。题型约束不满足返回 400，问卷关闭、到期或禁止的重复提交返回 409。`END` 表示结束填写，规则只能指向后续题目或 `END`，未命中规则则按顺序继续。

可定位的题目配置、逐题答案和整卷答案错误使用原问卷顺序中的“第 N 题”，重复选项进一步说明选项序号。不存在的答案键无法映射到题号时返回通用错误。题号用于提示；请求、响应和答卷键仍保留稳定 ID。

`anonymous=true` 只在问卷允许匿名时有效，否则返回 400。匿名提交的答卷不写 `userId`；本次 `anonymous=false` 时仍写入 `userId`，即使问卷允许匿名。`allowRepeat=false` 时，两种方式都按账号和问卷生成去重标记。匿名并不表示免登录，也不表示数据库层面完全不可关联。

## 七、使用情况与统计

### 1. 单份问卷统计

仅问卷创建者可以读取；其他用户返回 404。

| 方法 | 路径 | 结果 |
| --- | --- | --- |
| GET | /api/surveys/{id}/stats | 整卷汇总及每个填空题的第一页原始答案 |
| GET | /api/surveys/{id}/stats?summaryOnly=true | 整卷汇总，不加载原始答案 |
| GET | /api/surveys/{id}/stats?questionId=q2&offset=0&limit=20 | 指定题目的汇总和原始答案页 |

响应包含 `totalResponses`、`questions[].answered`、`options[].count`、`average`、`values`、`valuesOffset`、`valuesLimit`、`hasMore`。数字填空计算平均值，文本和数字内容通过 `values` 分页返回。指定题目时，`totalResponses` 仍是该问卷的总提交数。题目不存在返回 404。

`summaryOnly` 只能出现一次且值为 `true` 或 `false`。`offset` 默认为 0，须为非负整数；`limit` 默认为 20，范围为 1–50。重复分页参数或不合法参数返回 400。

`totalResponses` 计已保存的提交记录。`answered` 计该题答案字段存在且不为 `null` 的记录：合法空字符串、空数组也计入，数字 0 同时计入回答数及数值平均值；缺失和 `null` 不计入。多选的 `options[].count` 分别计各选项，空数组不会增加选项次数。允许重复提交时，这些数量不是去重人数。

### 2. 可复用题目的使用情况

| 方法 | 路径 | 结果 |
| --- | --- | --- |
| GET | /api/questions/{id}/usage | 本人问卷引用明细及引用数量 |

请求者需仍有该题目的读取权限。响应含 `ownSurveyCount`、`totalSurveyCount`、`totalOccurrenceCount` 和 `surveys`。每条明细包含 `surveyId`、`title`、`status`、`archived`、局部 `questionId` 和 `versionId`，并且仅来自本人问卷。

题目作者可以查看全体用户的引用总量；被共享用户的总量也仅限自己的问卷。全局总量不返回其他用户问卷的名称、ID、回答内容或逐版本引用明细。数量包括草稿、已关闭、已归档问卷中的现存引用，不要求已经有答卷。

明细中的一项对应一次题目出现。同一题目在一份问卷中使用两次，问卷数计一、出现次数计二。例如本人两份问卷分别引用一次和两次，`ownSurveyCount=2`，`surveys` 长度为 3。

### 3. 跨问卷单题统计

| 方法 | 路径 | 结果 |
| --- | --- | --- |
| GET | /api/questions/{id}/stats | 各版本统计摘要，不返回原始答案 |
| GET | /api/questions/{id}/stats?versionId=版本ID&offset=0&limit=20 | 指定版本的摘要及原始答案页 |

请求者需有该题目的当前读取权限，统计始终只纳入当前用户创建的问卷。题目作者也不能读取其他用户引用后产生的回答。结果按不可变版本分别计算，内容、题型或选项不同的版本不混算。

指定版本并使用 `limit=2` 查询三个数值 10、20、30 时，响应形如：

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

数字分布由 MongoDB 自适应分为最多 10 桶，`min` 包含下界、`max` 不包含上界，最后一桶包含上界；实际桶数和边界由数据决定。平均值直接对所有有效数值计算，不对各问卷均值再求平均。

`totalAnswers` 和每版本 `answered` 计回答的题目实例数，`surveyCount` 计实际包含这些回答的不同问卷数。缺失、`null` 和被跳过的答案不计入；合法空文本、空数组和数字 0 沿用单卷口径。重复提交，以及同一版本在同一问卷中的多次出现，均分别计入。

不指定 `versionId` 时始终只返回摘要，`values=[]`、`valuesLimit=0`；指定版本后才返回原始答案页。指定版本不属于该题目时返回 404，重复 `versionId` 返回 400。分页参数采用单卷统计相同的默认值、范围和重复参数检查。

原始答案按提交时间、答卷 ID、问卷 ID、局部题目 ID 稳定排序，并通过 `hasMore` 判断后续页。多次请求之间可能有新答案写入，因此不保证所有分页来自同一时点快照。统计模型和权限依据见 [数据库与核心逻辑设计](design.md)，验证场景见 [测试用例与结果](testing.md)。
