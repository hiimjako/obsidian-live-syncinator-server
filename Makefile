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

.PHONY: generate
generate:
	sqlc generate
