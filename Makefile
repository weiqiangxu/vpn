SERVER_BIN_NAME=gvpn-server
CLIENT_BIN_NAME=gvpn-client

build:
	@go build -o $(SERVER_BIN_NAME) ./gvpn/server.go
	@go build -o $(CLIENT_BIN_NAME) ./gvpn/client.go

linux:
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/$(SERVER_BIN_NAME) ./gvpn/server.go
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/$(CLIENT_BIN_NAME) ./gvpn/client.go

mac:
	@CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o bin/$(SERVER_BIN_NAME) ./gvpn/server.go
	@CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o bin/$(CLIENT_BIN_NAME) ./gvpn/client.go


clean:
	@rm -rf $(SERVER_BIN_NAME)
	@rm -rf $(CLIENT_BIN_NAME)
	@rm -rf bin

.PHONY: build