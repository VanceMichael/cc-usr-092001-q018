.PHONY: test fmt vet migrate run docker

test:
	go test ./...

fmt:
	gofmt -l -w internal cmd

vet:
	go vet ./...

migrate:
	sh scripts/migrate.sh

run:
	go run ./cmd/server

docker:
	docker build -t friendship-graph .
