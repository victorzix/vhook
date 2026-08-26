.PHONY: up down run generate test test-integration test-race

## up: sobe só a infraestrutura; a api roda local com `make run`
up:
	docker compose up -d postgres rabbitmq

down:
	docker compose down

run:
	go run ./cmd/api

generate:
	go tool sqlc generate
	go tool oapi-codegen -config contracts/oapi-codegen.yaml contracts/openapi.yaml

## test: só unidade — rápido o bastante para rodar a cada green
test:
	go test -short ./...

## test-integration: sobe container de verdade
test-integration:
	go test -shuffle=on ./...

## test-race: o que o CI roda. Exige CGO_ENABLED=1 e um compilador C, que
## o Windows não tem por padrão — por isso não é o alvo do dia a dia.
test-race:
	CGO_ENABLED=1 go test -race -shuffle=on ./...
