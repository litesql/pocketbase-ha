version = $(shell git describe --tags --abbrev=0 | sed 's/^v//')
.PHONY: docker-image
docker-image:
	docker build . -t ghcr.io/litesql/pocketbase-ha:$(version) --target production

.PHONY: docker-image-dev
docker-image-dev:
	docker build . -t ghcr.io/litesql/pocketbase-ha:dev --target production

.PHONY: release
release:
	goreleaser release --clean
