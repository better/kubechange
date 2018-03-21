NAME=kubechange

all: quality test build

quality:
	gofmt -w *.go
	go tool vet *.go

test:
	go test -coverprofile=coverage

build: build-darwin build-linux

build-%:
	GOOS=$* GOARCH=amd64 go build -o ${NAME}-$* main.go compare.go plan.go
