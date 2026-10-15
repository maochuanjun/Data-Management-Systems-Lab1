# 测试目录

| 路径 | 用途 |
| --- | --- |
| unit/ | 不依赖数据库的 Go 公开行为测试与 Python 项目配置加载测试 |
| integration/ | 真实MongoDB、HTTP、并发、容量与旧数据兼容 |
| frontend/ | Node前端事件与竞态回归 |
| e2e/ | 真实Chromium完整业务验收 |
| fixtures/v1/ | 固定历史数据契约，后续迭代不得用当前结构重新生成来替换 |
| support/ | 隔离数据库夹具与清理 |

从仓库根目录运行 `make test`。数据库测试从项目根目录 `.env` 加载配置；首次使用先复制 `.env.example` 为 `.env` 并填写 `MONGO_URI`，再运行 `python3 scripts/with-local-mongo.py make integration` 或 `python3 scripts/with-local-mongo.py make e2e`。shell 环境变量优先，`INTEGRATION_MONGO_URI` 默认继承 `MONGO_URI`。

工具配置、输入输出、执行证据和验证边界以 [测试说明](../documentation/testing.md) 为准。
