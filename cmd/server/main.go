package main

import (
	"log"
	"net/http"
	"os"

	"example.com/batch-092001-q018/internal/httpapi"
	"example.com/batch-092001-q018/internal/service"
)

func main() {
	svc := service.New()
	handler := httpapi.New(svc)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("友城关系图谱服务监听 :%s", port)
	if err := http.ListenAndServe("0.0.0.0:"+port, handler); err != nil {
		panic(err)
	}
}
