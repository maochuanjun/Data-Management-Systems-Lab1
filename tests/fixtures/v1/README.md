# 第一阶段历史数据样例

本目录保存固定的 MongoDB Extended JSON 数据，用于验证旧问卷、旧答卷在后续代码修改后仍能读取、填写和统计。

- user.json：仅供测试的账号。
- survey-draft.json、survey-published.json：旧版问卷，文本和数字填空曾分别使用 type=text/number。
- response.json：旧版已提交答卷。

tests/integration/compatibility_test.go 使用独立随机测试数据库导入这些文件，验证结构兼容及历史答卷不被更改。

第二阶段需要修改数据模型时，应保留这批样例作为回归基准，不要为了适配新代码直接覆盖旧数据样例。
