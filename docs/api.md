# HTTP API 参考

基础路径 `/v1`，请求与响应均为 `application/json; charset=utf-8`。

所有写端点的请求体形如：

```json
{
  "meta": { "event_id": "EVT-…", "occurred_at": "2026-09-01T10:00:00+08:00" },
  "payload": { }
}
```

`meta` 可整体省略：服务端自动生成事件标识、按来源自动分配连续序号，
`occurred_at` 取服务当前时刻（UTC）。携带相同 `event_id` 重试返回 `200`
且 `duplicated=true`，不产生第二条事实；首次成功为 `201`。

| 状态码 | 含义 |
| --- | --- |
| 201 | 事件已落盘 |
| 200 | 幂等重试 |
| 400 | 请求形状错误（未知字段、时间格式、引用前缀等） |
| 404 | 对象不存在 |
| 409 | 业务冲突（重复配对、状态机非法迁移、批准不齐等） |

## 写入端点

| 方法与路径 | 载荷类型 |
| --- | --- |
| `POST /v1/events` | 完整交换信封（见 `contracts/event.example.json`） |
| `POST /v1/entities` | `entity_registered` |
| `POST /v1/entities/{ref}/names` | `name_attached`（路径覆盖 `entity_ref`） |
| `POST /v1/entities/merge` | `entity_merged` |
| `POST /v1/relations` | `relation_proposed` |
| `POST /v1/relations/{ref}/approvals` | `approval_recorded` |
| `POST /v1/relations/{ref}/conclude` | `relation_concluded`（仅需日期字段） |
| `POST /v1/relations/{ref}/suspend` | `{at, reason?}` |
| `POST /v1/relations/{ref}/resume` | `{at}` |
| `POST /v1/relations/{ref}/terminate` | `{at, reason?}` |
| `POST /v1/relations/{ref}/texts` | `text_recorded` |
| `POST /v1/plans` | `plan_adopted` |
| `POST /v1/plans/{ref}/bindings` | `{relation_ref, at}` |
| `POST /v1/plans/{ref}/confirmations` | `{relation_ref, city_ref, at}` |
| `POST /v1/relations/{ref}/commitments` | `commitment_logged` |
| `POST /v1/commitments/{ref}/fulfill` | `commitment_fulfilled` |
| `POST /v1/relations/{ref}/activities` | `activity_held` |

## 查询端点

| 方法与路径 | 说明 |
| --- | --- |
| `GET /v1/entities?lang=zh&name=大阪` | 按名称（含历史名称、大小写不敏感）查实体 |
| `GET /v1/entities/{ref}` | 实体卷宗：名称历史、合并继受链、同名实体、相关关系 |
| `GET /v1/relations` | 全部关系 |
| `GET /v1/relations/{ref}` | 友城卷宗：批准、文本、计划约束、承诺（开放/逾期/兑现）、活动成果、责任部门 |
| `POST /v1/proposals/analyze` | 请求体 `{entity_a, entity_b}`，返回冲突、同名提示与可复用资源 |
| `GET /health` | 健康检查 |

## 示例：登记一对友城关系

```bash
curl -X POST localhost:8080/v1/entities -d '{
  "payload": {"ref":"ENT-SHA01","level":"city","parent_ref":"ENT-CN001",
              "names":{"zh":"上海市","en":"Shanghai"}}}'
curl -X POST localhost:8080/v1/entities -d '{
  "payload": {"ref":"ENT-OSA01","level":"city","parent_ref":"ENT-JP001",
              "names":{"zh":"大阪市","ja":"大阪市","en":"Osaka"}}}'
curl -X POST localhost:8080/v1/relations -d '{
  "payload": {"ref":"REL-SHAOSA","entity_a":"ENT-SHA01","entity_b":"ENT-OSA01",
              "responsible_dept":"上海市外办"}}}'
curl -X POST localhost:8080/v1/relations/REL-SHAOSA/approvals -d '{
  "payload": {"ref":"APR-SHA01","side":"a","decision":"approved",
              "authority":"上海市人大常委会","decided_at":"2026-01-10"}}'
curl -X POST localhost:8080/v1/relations/REL-SHAOSA/approvals -d '{
  "payload": {"ref":"APR-OSA01","side":"b","decision":"approved",
              "authority":"大阪市议会","decided_at":"2026-01-15"}}'
curl -X POST localhost:8080/v1/relations/REL-SHAOSA/conclude -d '{
  "payload": {"signing_date":"2026-02-01","effective_date":"2026-03-01"}}'
```
