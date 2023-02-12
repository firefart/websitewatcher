.DEFAULT_GOAL := build

.PHONY: update
update: protoc
	go get -u -t
	go mod tidy

.PHONY: build
build: test
	go fmt ./...
	go vet ./...
	go build

.PHONY: run
run: build
	./websitewatcher

.PHONY: dev
dev: build
	./websitewatcher -debug -dry-run -config config.json

.PHONY: lint
lint:
	"$$(go env GOPATH)/bin/golangci-lint" run ./...
	go mod tidy

.PHONY: lint-update
lint-update:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin
	$$(go env GOPATH)/bin/golangci-lint --version

.PHONY: test
test:
	go test -race -cover ./...

.PHONY: install-protoc
install-protoc:
	mkdir -p /tmp/protoc
	curl -s -L https://api.github.com/repos/protocolbuffers/protobuf/releases/latest | jq '.assets[] | select(.name | endswith("-linux-x86_64.zip")) | .browser_download_url' | xargs curl -s -L -o /tmp/protoc/protoc.zip
	unzip -d /tmp/protoc/ /tmp/protoc/protoc.zip
	sudo mv /tmp/protoc/bin/protoc /usr/bin/protoc
	sudo rm -rf /usr/local/include/google
	sudo mv /tmp/protoc/include/* /usr/local/include
	rm -rf /tmp/protoc

.PHONY: protoc
protoc: install-protoc
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	protoc -I ./proto -I /usr/local/include/ ./proto/database.proto --go_out=.
