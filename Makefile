NAME=kubechange

all: quality test build

quality:
	gofmt -w *.go
	go tool vet *.go

test:
	go test

build: build-darwin build-linux

build-%:
	GOOS=$* GOARCH=amd64 go build -o ${NAME}-$*
