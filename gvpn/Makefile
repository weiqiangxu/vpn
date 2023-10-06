SERVER_BIN_NAME=gvpn-server
CLIENT_BIN_NAME=gvpn-client

build:
	@go build -o $(SERVER_BIN_NAME) ./server.go
	@go build -o $(CLIENT_BIN_NAME) ./client.go

linux:
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/$(SERVER_BIN_NAME) ./server.go
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/$(CLIENT_BIN_NAME) ./client.go

mac:
	@CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o bin/$(SERVER_BIN_NAME) ./server.go
	@CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o bin/$(CLIENT_BIN_NAME) ./client.go


build-all:
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/$(BIN_NAME)-linux-amd64 main.go && \
    CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o bin/$(BIN_NAME)-darwin-amd64 main.go && \
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o bin/$(BIN_NAME)-win-amd64.exe main.go

clean:
	@rm -rf $(SERVER_BIN_NAME)
	@rm -rf $(CLIENT_BIN_NAME)
	@rm -rf bin

.PHONY: build