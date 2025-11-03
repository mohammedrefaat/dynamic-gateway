.PHONY: build run test clean docker

build:
	go build -o bin/gateway cmd/gateway/main.go

run:
	go run cmd/gateway/main.go -config configs/config.json

test:
	go test -v ./...

clean:
	rm -rf bin/

docker-build:
	docker build -t dynamic-gateway:latest .

docker-run:
	docker-compose up -d

proto:
	protoc --go_out=. --go-grpc_out=. proto/**/*.proto