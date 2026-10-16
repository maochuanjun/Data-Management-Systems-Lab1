# 基于 MongoDB 的在线问卷系统

《当代数据管理系统》第一次大作业（第一阶段）。项目使用 Go、MongoDB 和原生 HTML/CSS/JavaScript，实现问卷创建、填写、条件跳转和统计。重点是数据库设计与后端业务逻辑；界面保持简洁。

## 已实现功能

- 注册、登录、退出；用户只能管理和查看自己创建的问卷。
- 创建和编辑草稿，设置标题、说明、匿名填写及截止时间，发布、关闭问卷，并通过链接分享。
- **单选、多选、填空**三类题型；填空题再选择文本或数字。支持必填、选择数量、字符长度、数值范围和整数校验。
- 在问卷中配置单选、多选及数字填空的条件跳转；服务端根据答案计算路径并在提交时重新校验。
- 登录后填写问卷；按问卷设置选择匿名提交，以及是否允许同一账号重复提交。
- 创建者查看整卷或单题统计：选项次数、回答份数、数字平均值及填写内容。
- 额外提供草稿删除、问卷归档/恢复和关闭后重新开启，保留已提交答卷。

## 本地运行

需要 Go 1.26+、MongoDB、Python 3 和 Make。以下使用 Docker Compose 启动本地 MongoDB；已有 MongoDB 时可跳过第一行。

~~~sh
docker compose up -d
cp .env.example .env
make run
~~~

浏览器访问 http://127.0.0.1:8080。MongoDB 连接配置在项目根目录 `.env` 中，默认本地示例端口为 27018。`make run` 会加载 `.env`；直接执行 `go run ./cmd/server` 不会自动读取该文件。

## 演示流程

1. 注册账号 A，创建问卷，添加题目并设置校验和跳转规则。
2. 保存并发布，复制问卷链接。
3. 使用另一个浏览器会话登录账号 B，按规则填写并提交。
4. 账号 A 查看整卷统计和单题填写内容。
5. 尝试非法答案、重复提交和关闭问卷后的提交，检查错误提示。

新建填空题统一保存为 `type="fill"`，并通过 `fillType="text"` 或 `"number"` 区分；已有旧问卷中的 `text/number` 类型仍可读取、填写和统计。填写者必须登录；“匿名”表示答卷不直接公开或保存填写者账号 ID，不表示可以免登录。统计以提交记录计数，允许重复填写时不等于独立参与人数。分享给另一台电脑时，需要在可访问的主机上运行服务，不能直接使用本机的 127.0.0.1 链接。

## 测试

快速测试需要 Go、Python 3 和 Node.js，不连接 MongoDB：

~~~sh
make test
~~~

真实数据库集成测试需要 `.env` 中有可用的 MongoDB 配置；浏览器测试还需要 Playwright 和 Chromium：

~~~sh
python3 scripts/with-local-mongo.py make integration
python3 scripts/with-local-mongo.py make e2e
python3 scripts/with-local-mongo.py make verify
~~~

六类课程必需测试的步骤、输入、预期输出和历史验收记录见 [测试说明](documentation/testing.md)。

## 文件说明

| 路径 | 内容 |
| --- | --- |
| `cmd/server/` | 程序入口 |
| `internal/domain/` | 问卷模型、校验、条件跳转 |
| `internal/store/` | MongoDB Schema、索引、数据读写和统计 |
| `internal/web/` | HTTP 接口、认证、网页 |
| `tests/` | 单元、集成、前端和浏览器测试 |
| `scripts/` | 本地环境及测试辅助脚本 |
| `documentation/` | 数据库与系统设计、API、测试、AI 使用记录 |
| `项目完成报告.md` | 第一阶段课程报告 |

## 提交文档

- [项目完成报告](项目完成报告.md)
- [系统与 MongoDB 设计](documentation/design.md)
- [API 说明](documentation/api.md)
- [测试用例与记录](documentation/testing.md)
- [AI 使用日志](documentation/ai-log.md)

第二阶段尚未发布具体变更需求。现有代码保留稳定题目 ID、数据版本字段和历史数据回归测试，供后续在第一阶段基础上修改，而不是重建系统。
