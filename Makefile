.PHONY: backend agent test docker

backend:
	cd backend && go run ./cmd/server

agent:
	cd agent && go run ./cmd/agent

test:
	cd backend && go test ./...
	cd agent && go test ./...

docker:
	docker compose up -d --build
