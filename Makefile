.PHONY: build run test test-race cover fmt vet tidy clean \
	docker-init docker-build docker-up docker-down docker-restart docker-logs docker-ps docker-backup deploy \
	mp-build mp-preview mp-upload

BIN := bin/crab-server

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $(BIN) ./cmd/server

run:
	go run ./cmd/server

test:
	go test ./...

test-race:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

fmt:
	gofmt -s -w .

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin coverage.out

# ---------- Docker 部署 ----------
# 数据目录要归容器里的 uid 10001（镜像里的 crab 用户），否则 SQLite 写不进去。
# 需要 root：sudo make docker-init
docker-init:
	mkdir -p data backup
	chown -R 10001:10001 data backup

docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-restart:
	docker compose restart api

docker-logs:
	docker compose logs -f api

docker-ps:
	docker compose ps

docker-backup:
	./scripts/docker-backup.sh

# 线上更新：备份 → 拉代码 → 构建 → 滚动替换 → /healthz 自检，不过就自动回滚
deploy:
	./scripts/deploy.sh

# ---------- 小程序（miniprogram/，详见 mini-deploy.md）----------

mp-build:
	cd miniprogram && npm run build:weapp

mp-preview: mp-build
	cd miniprogram && npm run ci:preview

mp-upload: mp-build
	cd miniprogram && npm run ci:upload
