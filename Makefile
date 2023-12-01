.DEFAULT_GOAL := build

.PHONY: update
update:
	go get -u -t
	go mod tidy

.PHONY: build
build: test
	go fmt ./...
	go vet ./...
	CGO_ENABLED=1 go build -o websitewatcher

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
