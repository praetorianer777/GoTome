# Query code generation. sqlc reads the migrations as the schema and the files
# in backend/queries, and writes backend/internal/db/sqlc, which is checked in.

# Pinned by digest, so the generated code only changes when a query does.
SQLC_IMAGE ?= sqlc/sqlc:1.31.1@sha256:70f53171d27b2424e9358869975455a6e955a5aa8e58a998a270a6e34e525537

DOCKER_SQLC = docker run --rm -u $(UID_GID) -v $(ROOT)/backend:/src -w /src $(SQLC_IMAGE)

.PHONY: sqlc
sqlc: ## Regenerate the query code from backend/queries
	$(DOCKER_SQLC) generate

.PHONY: sqlc-check
sqlc-check: ## Fail when the generated query code differs from what the queries produce
	@$(DOCKER_SQLC) diff \
		|| { echo "backend/internal/db/sqlc is out of date. Run make sqlc and commit the result."; exit 1; }
