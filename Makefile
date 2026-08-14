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

COMPOSE_EMBEDDED_NATS = deploy/docker-embedded-nats/docker-compose.yml
COMPOSE_EXTERNAL_NATS = deploy/docker-external-nats/docker-compose.yml
COMPOSE_EMBEDDED_NATS_FILECACHE = deploy/docker-embedded-nats-filecache/docker-compose.yml
COMPOSE_EMBEDDED_NATS_S3 = deploy/docker-embedded-nats-s3/docker-compose.yml
COMPOSE_EXTERNAL_NATS_S3 = deploy/docker-external-nats-s3/docker-compose.yml

.PHONY: docker-embedded-nats docker-embedded-nats-down verify-docker-embedded-nats
docker-embedded-nats:
	docker compose -f $(COMPOSE_EMBEDDED_NATS) up --build
docker-embedded-nats-down:
	docker compose -f $(COMPOSE_EMBEDDED_NATS) down -v
verify-docker-embedded-nats:
	./deploy/scripts/verify-cluster.sh
	./deploy/scripts/verify-files.sh

.PHONY: docker-external-nats docker-external-nats-down verify-docker-external-nats
docker-external-nats:
	docker compose -f $(COMPOSE_EXTERNAL_NATS) up --build
docker-external-nats-down:
	docker compose -f $(COMPOSE_EXTERNAL_NATS) down -v
verify-docker-external-nats:
	./deploy/scripts/verify-cluster.sh
	./deploy/scripts/verify-files.sh

.PHONY: docker-embedded-nats-filecache docker-embedded-nats-filecache-down verify-docker-embedded-nats-filecache
docker-embedded-nats-filecache:
	docker compose -f $(COMPOSE_EMBEDDED_NATS_FILECACHE) up --build
docker-embedded-nats-filecache-down:
	docker compose -f $(COMPOSE_EMBEDDED_NATS_FILECACHE) down -v
verify-docker-embedded-nats-filecache:
	./deploy/scripts/verify-cluster.sh
	./deploy/scripts/verify-files.sh

.PHONY: docker-embedded-nats-s3 docker-embedded-nats-s3-down verify-docker-embedded-nats-s3
docker-embedded-nats-s3:
	docker compose -f $(COMPOSE_EMBEDDED_NATS_S3) up --build
docker-embedded-nats-s3-down:
	docker compose -f $(COMPOSE_EMBEDDED_NATS_S3) down -v
verify-docker-embedded-nats-s3:
	./deploy/scripts/verify-cluster.sh
	./deploy/scripts/verify-files.sh

.PHONY: docker-external-nats-s3 docker-external-nats-s3-down verify-docker-external-nats-s3
docker-external-nats-s3:
	docker compose -f $(COMPOSE_EXTERNAL_NATS_S3) up --build
docker-external-nats-s3-down:
	docker compose -f $(COMPOSE_EXTERNAL_NATS_S3) down -v
verify-docker-external-nats-s3:
	./deploy/scripts/verify-cluster.sh
	./deploy/scripts/verify-files.sh
