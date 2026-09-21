# 国际友城合作关系图谱

管理国家、省州、城市及其历史名称的稳定标识，跟踪友好关系从意向、正式缔结、
暂停到终止的全过程，保存双方法定批准、多语文本版本、生效日期与文化、旅游、
教育、青年等合作事项，并沿友城对汇总承诺、活动成果、逾期事项与责任部门，
为新合作提议发现现有网络中的重叠、冲突与可复用资源。

## 设计要点

- **事件溯源**：所有事实以追加事件落盘（JSONL，逐行 `sha256` 摘要），
  当前状态由重放得到；更名、合并、暂停、终止均不删除历史。
- **稳定标识 + 历史名称**：实体标识不复用；更名只追加名称；合并保留继受链，
  旧关系不移动、不删除，长期可追溯。
- **唯一友城对**：关系两侧为无序实体对，重复上报（反序、更名、合并后）不创建第二对关系。
- **双边批准闸门**：双方法定批准齐备方可正式缔结。
- **上级约束不替代城市确认**：路线图可按行政隶属挂接下级交往，但须城市分别确认。
- **多语与跨时区**：原文/译文/签署版本并存；活动时刻保留偏移量与 IANA 时区，
  逾期按 UTC 绝对时刻判定。
- **幂等交换**：`event_id` 幂等，`source` 内 `source_sequence` 连续，摘要不符即拒。

## 目录

- `cmd/server/` 服务入口。
- `internal/domain/` 标识、枚举、时间与多语基础约定。
- `internal/events/` 全部事实事件与载荷校验。
- `internal/eventlog/` JSONL 追加日志、幂等、序号水位、摘要复核、重放。
- `internal/graph/` 读模型投影：状态机、合并继受、卷宗、提议分析。
- `internal/store/` 日志与投影的单一读写入口。
- `internal/service/` 业务编排与错误分类。
- `internal/api/` HTTP 接口。
- `contracts/` 交换信封与请求示例。
- `docs/domain.md` 领域规则；`docs/api.md` HTTP API 参考。

## 运行

```sh
make test       # 全部单元与端到端测试
make migrate    # 初始化事件日志目录（DATABASE_PATH，默认 data/graph-events.jsonl）
make run        # 启动服务，默认 :8080，健康检查 GET /health
```

环境变量：`PORT`（默认 8080）、`DATABASE_PATH`（默认 `data/graph-events.jsonl`）、
`EVENT_SOURCE`（本端来源标识，默认 municipal-affairs-office）。

接口说明见 `docs/api.md`，领域规则见 `docs/domain.md`。
事件日志文件与本地数据不得提交到仓库。
