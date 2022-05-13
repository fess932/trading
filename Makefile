include .env
export

exchange:
	go run ./cmd/exchange

broker:
	go run ./cmd/broker

client:
	go run ./cmd/client


tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

gen:
	@protoc --version
	@protoc-gen-go --version
	@protoc-gen-go-grpc --version

	protoc --go_out=. --go-grpc_out=. api/proto/exchange.proto

.PHONY: exchange stockbrocker