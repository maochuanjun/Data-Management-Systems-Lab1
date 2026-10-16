# 在线问卷系统（第一阶段）

课程项目：使用 Go 和 MongoDB 实现一个简化的在线问卷系统。填写者需要先登录；问卷创建者可以设计、发布和关闭问卷，并查看统计结果。

## 已实现功能

- 用户注册、登录、退出；创建者只能管理自己的问卷。
- 创建草稿、编辑题目、发布、设置截止时间、关闭及分享问卷链接。
- 单选、多选、文本和数字题；必填、数量、长度、范围及整数校验。
- 单选、多选和数字题的可配置条件跳转；服务器在提交时重新校验整条作答路径。
- 匿名提交和是否允许同账号重复提交的设置。
- 整卷或单题统计：选项次数、回答份数、文本填写内容、数字平均值；原始内容按需分页。
- 使用 MongoDB 保存全部业务数据与会话，不使用关系数据库或现成问卷系统。

## 本地运行

依赖：Go 1.26+、MongoDB、Python 3、Make；使用下面的容器示例时还需要 Docker Compose。

```sh
docker compose up -d            # 可选：启动本项目的 MongoDB（本机端口 27018）
cp -n .env.example .env
chmod 600 .env
make run
```

默认访问地址：<http://127.0.0.1:8080>。首次克隆需按自己的 MongoDB 环境修改项目根目录的 `.env`；不要提交数据库账号或密码。`make run` 会加载项目的 `.env`，但直接执行 `go run ./cmd/server` 不会自动读取它。

跨电脑演示时，可在受信任的局域网内设置 `LISTEN_ADDR=0.0.0.0`，并从其他电脑访问服务所在主机的局域网 IP；分享链接将基于浏览器实际打开的地址生成。默认的 `127.0.0.1` 链接不能被另一台电脑直接访问。不要把未配置访问控制的 MongoDB 或明文 HTTP 服务直接暴露到公网；正式 HTTPS 部署需设置 `COOKIE_SECURE=true`。

## 演示步骤

1. 注册账号 A，创建草稿并添加题目，设置必填和条件跳转。
2. 保存并发布，复制问卷链接。
3. 使用另一个浏览器会话注册账号 B，登录后按跳转规则填写、提交。
4. 账号 A 查看整卷或单题统计；文本和数字原始答案可按需加载并翻页。
5. 验证非法答案、重复提交、关闭或到期后的提交会被拒绝。

## 测试

快速测试需要 Go、Python 3、Node.js，不需要 MongoDB：

```sh
make test
```

真实数据库测试需要配置 `MONGO_URI`；浏览器验收还需要 Playwright 和 Chromium：

```sh
python3 scripts/with-local-mongo.py make integration
python3 scripts/with-local-mongo.py make e2e
python3 scripts/with-local-mongo.py make verify
```

数据库测试只使用自动创建的独立测试库，不修改正常项目数据。具体环境、用例及历史执行结果见 [测试说明](documentation/testing.md)。`make test` 不能代替真实数据库和浏览器验收。

## 项目结构

```text
cmd/server/              程序入口
internal/domain/         模型、题目校验、跳转与作答路径
internal/store/          MongoDB 集合、索引、读写与统计
internal/web/            HTTP 接口、认证与静态前端
tests/                   单元、集成、前端及浏览器测试
tests/fixtures/v1/       第一阶段历史数据兼容用例
scripts/                 本地配置与浏览器测试辅助脚本
documentation/           设计、API、测试及 AI 使用记录
项目完成报告.md           第一阶段项目完成报告
```

## 文档

- [项目完成报告](项目完成报告.md)
- [系统与 MongoDB 设计](documentation/design.md)
- [HTTP API](documentation/api.md)
- [测试用例与执行记录](documentation/testing.md)
- [AI 使用记录](documentation/ai-log.md)

## 当前约束

发布后问卷题目及配置不可编辑；条件跳转只允许向后跳或结束，避免循环。匿名提交不在答案中直接保存账号 ID，但仅提供业务界面的匿名性，不等同于数据库管理员无法关联身份。统计以提交份数计数，允许重复时不代表独立人数。关闭与同时写入的请求不是严格事务原子边界。

第二阶段应在现有代码基础上增量扩展，保留已有结构、接口测试和 v1 数据兼容测试，不把第一阶段推倒重做。
