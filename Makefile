.PHONY: build run test test-race cover fmt vet tidy clean mp-build mp-preview mp-upload

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

# ---- 小程序（miniprogram/，详见 README「小程序上传」）----

mp-build:
	cd miniprogram && npm run build:weapp

mp-preview: mp-build
	cd miniprogram && npm run ci:preview

mp-upload: mp-build
	cd miniprogram && npm run ci:upload
