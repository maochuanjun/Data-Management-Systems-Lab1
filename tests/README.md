# 测试目录

| 路径 | 用途 |
| --- | --- |
| unit/ | 不依赖数据库的公开行为单元测试 |
| integration/ | 真实MongoDB、HTTP、并发、容量与旧数据兼容 |
| frontend/ | Node前端事件与竞态回归 |
| e2e/ | 真实Chromium完整业务验收 |
| fixtures/v1/ | 固定历史数据契约，后续迭代不得用当前结构重新生成来替换 |
| support/ | 隔离数据库夹具与清理 |

从仓库根目录运行 `make test`；使用现有数据库配置运行 `python3 scripts/with-local-mongo.py make integration` 或 `make e2e`。

工具配置、输入输出、执行证据和验证边界以 [测试说明](../documentation/testing.md) 为准。
