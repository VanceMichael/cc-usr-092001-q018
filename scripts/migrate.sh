#!/bin/sh
set -eu

# 事件日志为 JSONL 追加文件，无需建表；本脚本只确保目录存在。
# 路径由 DATABASE_PATH 指定，与服务运行时保持一致。
DATABASE_PATH="${DATABASE_PATH:-data/graph-events.jsonl}"
mkdir -p "$(dirname "$DATABASE_PATH")"
if [ ! -f "$DATABASE_PATH" ]; then
	: > "$DATABASE_PATH"
	echo "已初始化空事件日志: $DATABASE_PATH"
else
	echo "事件日志已存在: $DATABASE_PATH（已有 $(wc -l < "$DATABASE_PATH") 条事件，启动时自动重放）"
fi
