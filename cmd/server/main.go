package main

import (
	"log"
	"net/http"
	"os"

	"example.com/batch-092001-q018/internal/api"
	"example.com/batch-092001-q018/internal/service"
	"example.com/batch-092001-q018/internal/store"
)

func main() {
	// DATABASE_PATH 指向 JSONL 事件日志；缺省使用本地 data 目录。
	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "data/graph-events.jsonl"
	}
	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("打开事件日志失败: %v", err)
	}
	svc := service.New(st, os.Getenv("EVENT_SOURCE"))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("友城关系图谱服务启动，事件日志 %s，监听 :%s", dbPath, port)
	if err := http.ListenAndServe("0.0.0.0:"+port, api.New(svc)); err != nil {
		log.Fatalf("服务退出: %v", err)
	}
}
