# MongoDB 数据库与核心逻辑设计

本文件对应第一次大作业第一阶段，重点回答三个问题：**数据怎样存、为什么采用这种结构、数据库与后端如何共同保证业务正确性**。

## 一、系统结构

~~~text
浏览器（创建、填写、统计）
          ↓ HTTP/JSON
internal/web    会话、权限、接口
          ↓
internal/domain 问卷结构、答案校验、条件跳转
          ↓
internal/store  MongoDB CRUD、索引、统计聚合
          ↓
        MongoDB
~~~

程序入口是 cmd/server/main.go。前端不直接操作数据库；所有持久化数据均进入 MongoDB，不使用关系数据库或第三方问卷系统。

## 二、集合划分与数据关联

| 集合 | 关键字段 | 作用及关系 |
| --- | --- | --- |
| users | _id、username、passwordHash、createdAt | 保存账号；密码为 bcrypt 哈希 |
| sessions | _id、userId、expiresAt | 登录会话；_id 是令牌哈希，userId 指向用户 |
| surveys | _id、ownerId、title、description、status、deadline、questions、revision、schemaVersion | 保存问卷；ownerId 指向创建者；题目、选项和规则嵌入 questions |
| responses | _id、surveyId、answers、path、submittedAt、anonymous、schemaVersion | 一份提交对应一个文档；surveyId 关联问卷 |

**嵌入 questions：** 问卷创建、编辑、发布和填写时需要同时读取题目、选项及跳转规则。嵌入同一文档可一次取得完整定义，也便于对草稿整体更新。题目上限为 100 道，限制单份问卷的增长。

**独立 responses：** 同一问卷可能有大量提交。若把答卷嵌入 surveys，文档会随提交不断增长，容易触及单文档大小限制，并造成多名填写者争用同一文档。因此，每次填写插入一份独立的 response，可按 surveyId 检索和聚合。

**独立 users、sessions：** 账号、会话过期与业务问卷的生命周期不同，不适合嵌入每份问卷。不同集合之间使用应用层 ID 关联，而非关系数据库外键。

## 三、题目与答卷的表示

业务题型只有三类：

| 题型 | 存储表示 | 主要约束 |
| --- | --- | --- |
| 单选 | type=single | 选项 ID、必填 |
| 多选 | type=multiple | 选项 ID 数组、minSelect、maxSelect |
| 填空 | type=fill，fillType=text/number | 文本 minLength/maxLength；数字 min/max/integer |

新建问卷的简化示例（省略说明字段、时间及其他可选属性）：

~~~json
{
  "_id": "survey_001",
  "ownerId": "user_001",
  "schemaVersion": 1,
  "revision": 1,
  "title": "课程反馈",
  "status": "published",
  "allowAnonymous": true,
  "allowRepeat": false,
  "questions": [
    {
      "id": "q1", "title": "是否继续填写？",
      "type": "single", "required": true,
      "options": [
        {"id": "yes", "label": "是"},
        {"id": "no", "label": "否"}
      ],
      "validation": {"integer": false},
      "rules": [{"operator": "eq", "value": "no", "target": "END"}]
    },
    {
      "id": "q2", "title": "年龄",
      "type": "fill", "fillType": "number", "required": true,
      "options": [],
      "validation": {"min": 0, "max": 120, "integer": true},
      "rules": []
    }
  ]
}
~~~

选“否”时路径只有 q1；选“是”时默认继续回答 q2。提交后的答卷包含：

~~~json
{
  "_id": "response_001",
  "surveyId": "survey_001",
  "schemaVersion": 1,
  "anonymous": true,
  "answers": {"q1": "yes", "q2": 21},
  "path": ["q1", "q2"],
  "submittedAt": "2026-10-17T08:00:00Z"
}
~~~

示例中时间以 JSON 字符串展示；真正写入 MongoDB 时使用 BSON Date。匿名答卷不保存 userId。禁止重复提交时，数据库另存由问卷 ID 和登录用户 ID 生成的 dedupeKey，借助唯一索引判断同一账号是否已提交。这种匿名**不是不可关联的密码学匿名**，不能将其描述为完全无法追踪身份。

## 四、MongoDB Schema 与索引

实现位置：**internal/store/mongo.go** 中的 collectionSchemas()、Init()。

初始化建立四个集合的 MongoDB JSON Schema 校验器，对 BSON 类型、必要字段、问卷状态、题型枚举、数据版本等做基础约束。主要约束如下：

| 对象 | MongoDB 层约束 | 后端继续检查 |
| --- | --- | --- |
| users | 用户名、密码哈希、创建时间字段类型 | 注册格式、密码长度、bcrypt 校验 |
| surveys | 必需字段、状态枚举、questions 数组及题型结构 | 标题、选项唯一性、题目数量、跳转目标及约束 |
| responses | surveyId、answers、path、时间等字段类型 | 填写者权限、问卷状态、实际作答路径、答案类型与范围 |
| sessions | userId 和 expiresAt 等字段类型 | 会话是否已经过期 |

注意：MongoDB Schema 并没有验证所有跨字段业务条件。例如“fill 必须有 fillType”“跳转只能指向后续题目”由 Go 的 ValidateSurvey() 完成。这是**数据库基础约束 + 应用层语义校验**的分工，不应声称数据库 Schema 已涵盖全部规则。

| 集合 | 索引 | 服务的操作 |
| --- | --- | --- |
| users | username 唯一 | 防止重复用户名 |
| sessions | expiresAt TTL | 清理过期会话；读取时仍检查是否过期 |
| surveys | ownerId、createdAt（降序） | 查询某用户创建的问卷 |
| responses | surveyId、submittedAt、_id | 按问卷查找和稳定分页原始答案 |
| responses | surveyId、dedupeKey 部分唯一 | 不允许重复填写时的并发去重 |

在 MongoDB 中，题目数组是主要的嵌入结构；答卷通过 surveyId 建立引用关系。相较于将问卷、题目、选项、跳转规则拆成多张关系表，这更接近“整卷读取、整卷编辑”的访问方式。代价是题目作为整体更新，不适合无限增长或高度独立地修改每道题。对于课程规定的问卷规模，这个取舍是合理的。

## 五、创建、发布和填写的数据流

1. **创建：** 注册用户调用 POST /api/surveys；存储层为 surveys 插入 draft 文档。
2. **编辑：** PUT /api/surveys/{id}。后端先运行 ValidateSurvey()，再用 _id、ownerId、status=draft 和 revision 作为更新条件；并发修改造成版本变化时返回 409。
3. **发布：** 检查非空题目、规则结构和截止时间，将状态从 draft 改为 published。已发布的题目和规则不能再编辑。
4. **填写：** GET /api/public/surveys/{id} 获取公开问卷。POST /next 对当前答案校验并计算下一题；服务端不依赖页面自己决定路径。
5. **提交：** POST /responses 执行 ValidateSubmission()，从第一题重新推导路径，检查每道实际经过的题、拒绝携带跳过题的答案，最后将 response 插入 MongoDB。
6. **关闭：** 将状态改为 closed，新的正常提交不再被接受；历史 responses 保留供统计使用。

## 六、条件跳转的保存与执行

规则保存在 surveys.questions[].rules，每条包含 operator、value 和 target。支持：

| 题型 | operator | value |
| --- | --- | --- |
| 单选 | eq | 一个选项 ID |
| 多选 | contains、contains_all | 一个选项 ID 或选项 ID 数组 |
| 数字填空 | eq、gt、gte、lt、lte | 数值 |

执行顺序是：先校验当前答案，再依次查规则；**第一条命中**的规则确定下一题；没有命中时继续下一题。target 只能是后续题目 ID 或 END，不允许回跳形成循环。真正的路径计算位于 **internal/domain/logic.go** 的 NextQuestion()；最终提交由 ValidateSubmission() 再计算一遍，而非接受浏览器传回的作答路径。

## 七、答案校验与重复提交

- 单选：答案属于已有选项；必答题不可为空。
- 多选：答案必须是选项数组、不能重复，满足最少/最多选择数和必答条件。
- 文本填空：校验字符长度、必答；使用 Unicode 字符计数。
- 数字填空：校验有限数值、上下限以及整数要求。
- 题目配置本身：检查选项、规则合法性，并拒绝“必答但无任何合法答案”的配置。

业务校验在 **internal/domain/logic.go** 中实现。页面上的即时错误提示只是交互帮助；保存最终以服务端为准。

重复提交由 MongoDB 的部分唯一索引控制，而不是先查询“是否存在”，从而避免两个并发请求都查到“尚未填写”后重复写入的竞态。不同账号的提交可作为独立文档写入。

## 八、统计为什么由 MongoDB 完成

实现位置：**internal/store/mongo.go** 的 Stats() 和 answerValues()。

- 首先按 surveyId 使用 $match 筛出该问卷的 responses。
- 通过 $facet 分别统计提交总数和每道题的回答份数。
- 使用 $group / $sum 统计选择次数；数字填空使用 $avg 计算平均值。
- 文本或数字的原始内容单独按 submittedAt、_id 排序分页获取，不把全部原始答案塞进单个聚合结果。
- 接口可以返回整卷统计，也可以传 questionId 查看指定题目的统计。

统计口径必须说明：totalResponses 是**提交记录数**；answered 是该题有答案的**提交份数**。允许重复提交时，不能把两者写成“去重的参与人数”。被跳过的题目不计作答。

## 九、第一阶段的范围及后续修改基础

已保留 schemaVersion、revision、稳定的题目和选项 ID，以及 tests/fixtures/v1 中的历史数据样例。旧问卷可能将文本、数字填空直接表示为 type=text/number；现有后端仍能读取这类数据，新建题目统一使用 type=fill 和 fillType。这属于已有数据的兼容处理，**不是**第二阶段要求的自动迁移实现。

第二阶段需求尚未发布，不能提前假设需增加哪些字段、索引或功能。届时应在此基础上说明需求变化、旧模型遇到的限制、所作调整、兼容策略和回归测试。

已知边界：问卷关闭与已进入提交处理的并发请求不是单个 MongoDB 事务；汇总聚合和原文分页查询也不保证同一时间点快照。本课程版本不实现循环跳转或通用数据迁移框架。
