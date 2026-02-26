DOCKER_TAG?=$(shell git log --format="%H" -n 1)
DOCKER_PLATFORM?=linux/amd64,linux/arm64
DOCKER_BUILDX_BUILD?=docker buildx build --push --platform $(DOCKER_PLATFORM) -t

fmt:
	go fmt ./...
.PHONY: fmt

lint:
	golangci-lint run
.PHONY: lint

test:
	go test ./...
.PHONY: test

check: fmt lint test
.PHONY: check

snapshot:
	UPDATE_SNAPS=true go test ./...
.PHONY: snapshot

build:
	go build -o ./build/patchbin ./cmd/patchbin
.PHONY: build

bp-setup:
	docker buildx ls | grep pico || docker buildx create --name pico
	docker buildx use pico
.PHONY: bp-setup

bp: bp-setup
	$(DOCKER_BUILDX_BUILD) ghcr.io/picosh/patchbin:$(DOCKER_TAG) --target release .
.PHONY: bp

smol:
	curl https://pico.sh/smol.css -o ./static/smol.css
.PHONY: smol

backup:
	scp pico.ash.3:git-pr/data/git-pr/data/pr.db ./data/prod.db
.PHONY: backup
