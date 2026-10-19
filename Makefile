BIN      := bin/snake-ladder
LDFLAGS  := -s -w

.PHONY: run test build build-linux deploy

run:
	go run ./cmd/server -addr :8080 -data ./data

test:
	go vet ./...
	go test -race ./...

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/server

# Static binary for the Hetzner box.
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)-linux-amd64 ./cmd/server

deploy: build-linux
	ansible-playbook -i deploy/ansible/inventory.ini deploy/ansible/playbook.yml
