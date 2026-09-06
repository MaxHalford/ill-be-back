.PHONY: run frontend check test build

run: frontend
	go run ./cmd/server -dev

frontend:
	cd frontend && npm ci && npm run build

check:
	go vet ./...
	cd frontend && npm run typecheck

test:
	go test -race ./...

build: frontend
	go build -o bin/ill-be-back ./cmd/server
