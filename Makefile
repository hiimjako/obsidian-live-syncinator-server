.PHONY: run
run:
	@go run cmd/main.go

.PHONY: test
test:
	go test ./... -count 1 -race -timeout 30s

.PHONY: bench
bench:
	go test ./... -count 1 -bench . -benchmem -run none        

.PHONY: lint
lint:
	golangci-lint run --fix
	npx prettier-pnp --pnp prettier-plugin-sql --write ./internal/migration/migrations/*.sql --config .github/config/.prettierrc

.PHONY: docker-build
docker-build:
	docker buildx build --platform linux/amd64,linux/arm64 --tag ghcr.io/hiimjako/obsidian-live-syncinator-server:local --file .docker/Dockerfile .

.PHONY: generate
generate:
	sqlc generate
