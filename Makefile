.PHONY: run
run:
	@go run cmd/main.go

.PHONY: editor
editor:
	@go run cmd/editor/* 2>editor.log

.PHONY: test
test:
	go test ./... -count 1 -race -timeout 30s

.PHONY: lint
lint:
	golangci-lint run --fix
	npx prettier-pnp --pnp prettier-plugin-sql --write ./internal/migration/migrations/*.sql --config .github/config/.prettierrc

.PHONY: generate
generate:
	sqlc generate




