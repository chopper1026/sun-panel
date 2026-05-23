.PHONY: dev backend frontend test-backend

dev:
	./scripts/dev.sh

backend:
	cd service && GOPROXY=$${GOPROXY:-https://goproxy.cn,direct} go run main.go

frontend:
	npx pnpm@8.15.9 dev

test-backend:
	cd service && GOPROXY=$${GOPROXY:-https://goproxy.cn,direct} go test ./...
