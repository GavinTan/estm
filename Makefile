export PATH := $(GOPATH)/bin:$(PATH)
export GO111MODULE=on
LDFLAGS := -s -w

all: build

build:
	env CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -tags estm -o bin/estm

env:
	@go version

clean:
	rm -rf ./bin