.PHONY: verify verify-backend verify-frontend verify-fork-invariants verify-docker

verify: verify-fork-invariants verify-backend verify-frontend

verify-fork-invariants:
	node scripts/verify-fork-invariants.cjs

verify-backend:
	go test ./cmd/... ./internal/...

verify-frontend:
	npm --prefix ./web ci
	npm --prefix ./web run test
	npm --prefix ./web run lint
	npm --prefix ./web run typecheck
	npm --prefix ./web run build

verify-docker:
	docker build -t cpa-usage-keeper:ci .
