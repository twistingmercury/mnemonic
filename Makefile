OPA_VERSION   ?= 1.4.2
ENVOY_IMAGE   ?= envoyproxy/envoy:v1.34.1
COMPOSE_FILE  := deploy/docker-compose.yaml
STUB_COMPOSE  := docker compose -f gateway/test/compose.stub.yaml

.PHONY: help test-policy validate-envoy up down e2e e2e-stub

help: ## Show targets
	@grep -E '^[a-z0-9-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

# Runs OPA from the pinned image, like validate-envoy, so a local run and a CI
# run use the same OPA version and need no host install.
OPA_IMAGE     ?= openpolicyagent/opa:$(OPA_VERSION)-envoy-static
OPA_RUN       := docker run --rm -v $(CURDIR)/gateway/opa/policy:/policy:ro $(OPA_IMAGE)

test-policy: ## Format-check, type-check and unit-test the OPA policies (pinned OPA image)
	$(OPA_RUN) fmt --list --fail /policy
	$(OPA_RUN) check --strict /policy
	$(OPA_RUN) test -v /policy

validate-envoy: ## Validate both Envoy configs (edge and egress)
	docker run --rm -v $(CURDIR)/gateway/envoy:/cfg:ro $(ENVOY_IMAGE) envoy --mode validate -c /cfg/envoy.yaml
	docker run --rm -v $(CURDIR)/gateway/envoy:/cfg:ro -e MNEMONIC_OPENAI_API_EMBEDDINGS_KEY=validate-only $(ENVOY_IMAGE) envoy --mode validate -c /cfg/envoy-egress.yaml

up: ## Start the full stack (needs the Mnemonic images)
	docker compose -f $(COMPOSE_FILE) up -d --wait

down: ## Stop the full stack
	docker compose -f $(COMPOSE_FILE) down

e2e: ## Run the allow/deny tests against the running stack
	cd gateway/test/e2e && go test -count=1 -v ./...

e2e-stub: ## Start the gateway with stub services, run the tests, stop it
	$(STUB_COMPOSE) up -d --build --wait --wait-timeout 240
	cd gateway/test/e2e && go test -count=1 -v ./... ; status=$$? ; cd ../../.. ; $(STUB_COMPOSE) down ; exit $$status

# ------------------------------------------------------------------ Services
# The Go module lives in src/ (cmd/api, cmd/mcp, cmd/enricher).

GO_SRC       := src
GO_BIN       := $(GO_SRC)/.bin
GIT_COMMIT   := $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo unknown)
GIT_TAG      := $(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)
BUILD_DATE   := $(shell date -u +%Y-%m-%d)
GO_MODULE    := github.com/twistingmercury/mnemonic
# Packages that carry `integration`-tagged tests (Postgres and Neo4j).
INTEGRATION_PKGS := ./internal/api/repository/... ./internal/api/service/... \
	./internal/mcp/repository/... ./internal/enricher/repository/...

.PHONY: build build-api build-mcp build-enricher local analyze tests-unit tests-bench tests-integration docs-swagger

# The scripts build the image and then run its end-to-end suite in containers;
# they never push and never use the build cache.
build: build-api build-mcp build-enricher ## Build the api, mcp and enricher images

build-api: ## Build the api image and run its e2e suite
	build/api/api.build.sh

build-mcp: ## Build the mcp image and run its e2e suite
	build/mcp/mcp.build.sh

build-enricher: ## Build the enricher image and run its e2e suite
	build/enricher/enricher.build.sh

local: ## Build the api, mcp and enricher binaries into src/.bin (no tests, no Docker)
	mkdir -p $(GO_BIN)
	for svc in api mcp enricher; do \
		go -C $(GO_SRC) build -ldflags "-s -w \
			-X '$(GO_MODULE)/internal/$${svc}/version.version=$(GIT_TAG)' \
			-X '$(GO_MODULE)/internal/$${svc}/version.buildDate=$(BUILD_DATE)' \
			-X '$(GO_MODULE)/internal/$${svc}/version.commit=$(GIT_COMMIT)'" \
			-o .bin/mnemonic-$${svc} ./cmd/$${svc} || exit 1; \
	done

# Runs the same checks as the image builds, with the host's tools. The gates
# inside the Dockerfiles stay authoritative. Unlike the Dockerfiles, goimports
# only lists unformatted files here, so the working tree is never rewritten.
analyze: ## Check formatting, then go vet, golangci-lint, govulncheck and gosec (host tools)
	cd $(GO_SRC) && unformatted="$$(goimports -l .)"; \
	if [ -n "$${unformatted}" ]; then echo "goimports: unformatted files:" >&2; echo "$${unformatted}" >&2; exit 1; fi
	cd $(GO_SRC) && go vet ./...
	cd $(GO_SRC) && golangci-lint run
	cd $(GO_SRC) && govulncheck ./cmd/... ./internal/...
	cd $(GO_SRC) && gosec -quiet -exclude-dir=tests ./...

tests-unit: ## Run the Go unit tests; coverage goes to src/.bin/coverage.out
	mkdir -p $(GO_BIN)
	cd $(GO_SRC) && go test -count=1 ./... -coverprofile=.bin/coverage.out

tests-bench: ## Run the Go benchmarks
	cd $(GO_SRC) && go test ./... -bench=. -benchmem -run='^$$'

# Does not start containers. Start the databases first with
#   docker compose -f db/docker-compose.yaml up -d --wait postgres neo4j
# then export (Postgres on 5435, Neo4j bolt on 7690 per that file):
#   MNEMONIC_TEST_DATABASE_URL, MNEMONIC_NEO4J_URI, MNEMONIC_NEO4J_USER,
#   MNEMONIC_NEO4J_PASSWORD, MNEMONIC_NEO4J_DATABASE
tests-integration: ## Run the integration tests (Postgres and Neo4j graph); needs MNEMONIC_TEST_DATABASE_URL and MNEMONIC_NEO4J_*, see the Makefile comment to start the databases
	@missing=""; \
	for name in MNEMONIC_TEST_DATABASE_URL MNEMONIC_NEO4J_URI MNEMONIC_NEO4J_USER MNEMONIC_NEO4J_PASSWORD MNEMONIC_NEO4J_DATABASE; do \
		eval "value=\$${$${name}:-}"; \
		if [ -z "$${value}" ]; then missing="$${missing} $${name}"; fi; \
	done; \
	if [ -n "$${missing}" ]; then \
		echo "ERROR: set these variables first:$${missing}" >&2; \
		echo "       Start the databases with: docker compose -f db/docker-compose.yaml up -d --wait postgres neo4j" >&2; \
		exit 1; \
	fi
	cd $(GO_SRC) && go test -count=1 -tags integration $(INTEGRATION_PKGS)

# Same invocation as build/api/api.Dockerfile.
docs-swagger: ## Regenerate the api swagger into src/docs/api/swagger
	cd $(GO_SRC) && go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g cmd/api/main.go -o docs/api/swagger --parseInternal

# ------------------------------------------------------------------ Databases
# These start containers (the runners use db/docker-compose.yaml on ports
# 5435, 7477 and 7690) and clean them up when they finish.

.PHONY: db-build db-build-postgres db-build-neo4j db-test db-test-postgres db-test-neo4j

db-build: db-build-postgres db-build-neo4j ## Build the mnemonic-postgres and mnemonic-neo4j images

db-build-postgres: ## Build the mnemonic-postgres image
	db/postgres/build.sh

db-build-neo4j: ## Build the mnemonic-neo4j image
	db/neo4j/build.sh

db-test: db-test-postgres db-test-neo4j ## Run both database BATS suites (starts containers)

db-test-postgres: ## Run the PostgreSQL migration BATS suite (starts containers)
	db/tests/postgres-test-runner.sh

db-test-neo4j: ## Run the Neo4j migration BATS suite (starts containers)
	db/tests/neo4j-test-runner.sh

# ------------------------------------------------------------------ Lint
SHELL_FILES = git ls-files -z '*.sh' '*.bash' ':!tests/bats/test_helper/bats-*'

.PHONY: tests-bats lint-shell lint-docker lint-helm lint-md

tests-bats: ## Run the BATS tests for the build tooling (tests/bats)
	bats tests/bats

lint-shell: ## shellcheck every tracked shell script (not the vendored BATS libraries)
	$(SHELL_FILES) | xargs -0 shellcheck

# hadolint exits non-zero on info-level findings, and the accepted DL3059
# notes (consecutive RUN instructions kept apart on purpose) are info-level;
# the warning threshold fails only on warnings and errors.
lint-docker: ## hadolint the Dockerfiles (fails on warnings and errors only)
	hadolint --failure-threshold warning build/*/*.Dockerfile db/*/Dockerfile

lint-helm: ## helm lint --strict on the chart
	helm lint --strict deploy/helm/mnemonic

lint-md: ## markdownlint every tracked Markdown file with the root .markdownlint.json
	git ls-files -z '*.md' | xargs -0 markdownlint -c .markdownlint.json

# ------------------------------------------------------------------ Kubernetes
# The same stack on a dedicated minikube profile, from deploy/helm/mnemonic.
# Every kubectl and helm call names the profile's context, so the current
# kubectl context and any other cluster are never touched.

K8S_PROFILE    ?= mnemonic
K8S_NAMESPACE  ?= mnemonic
K8S_RELEASE    ?= mnemonic
K8S_CPUS       ?= 6
K8S_MEMORY     ?= 12288
K8S_TIMEOUT    ?= 15m
K8S_CHART      := deploy/helm/mnemonic
K8S_RUN_DIR    := $(CURDIR)/.k8s-forward
KUBECTL        := kubectl --context $(K8S_PROFILE)
HELM           := helm --kube-context $(K8S_PROFILE)

# The project's images: a copy built on the host is loaded into the node;
# without one the node pulls the published image.
K8S_PROJECT_IMAGES := \
	ghcr.io/twistingmercury/mnemonic-api:latest-dev \
	ghcr.io/twistingmercury/mnemonic-mcp:latest-dev \
	ghcr.io/twistingmercury/mnemonic-enricher:latest-dev \
	ghcr.io/twistingmercury/mnemonic-postgres:v1.0.0-dev \
	ghcr.io/twistingmercury/mnemonic-neo4j:v1.0.0-dev
# Third-party: loaded from the host cache when present, else pulled by the node.
K8S_PUBLIC_IMAGES := \
	$(ENVOY_IMAGE) \
	openpolicyagent/opa:$(OPA_VERSION)-envoy-static \
	keycloak/keycloak:26.2.5 \
	rabbitmq:4-management-alpine \
	curlimages/curl:8.10.1

# ConfigMaps built from the repo files; the chart mounts them by these names.
K8S_CM_ENVOY    := mnemonic-envoy
K8S_CM_EGRESS   := mnemonic-egress
K8S_CM_OPA      := mnemonic-opa-config
K8S_CM_POLICY   := mnemonic-opa-policy
K8S_CM_REALM    := mnemonic-keycloak-realm
K8S_OPENAI_SECRET := mnemonic-openai

.PHONY: k8s-cluster k8s-images k8s-config k8s-secret k8s-up k8s-forward k8s-forward-stop k8s-test k8s-down k8s-delete

# Every root-owned container on the host shares one inotify instance budget;
# when it runs out, kube-proxy exits with "too many open files" and Calico
# never comes up.
k8s-cluster: ## Create or start the mnemonic minikube profile (Calico CNI)
	@limit=$$(cat /proc/sys/fs/inotify/max_user_instances 2>/dev/null || echo 0); \
	if [ "$${limit}" -gt 0 ] && [ "$${limit}" -lt 512 ]; then \
		echo "WARNING: fs.inotify.max_user_instances is $${limit}; kube-proxy may fail with 'too many open files'." >&2; \
		echo "         Raise it with: sudo sysctl -w fs.inotify.max_user_instances=512" >&2; \
	fi
	@if minikube -p $(K8S_PROFILE) status >/dev/null 2>&1; then \
		echo "minikube profile $(K8S_PROFILE) is running"; \
	else \
		echo "starting minikube profile $(K8S_PROFILE) (docker driver, Calico, $(K8S_CPUS) CPUs, $(K8S_MEMORY) MB)"; \
		minikube start -p $(K8S_PROFILE) --driver=docker --cni=calico \
			--cpus=$(K8S_CPUS) --memory=$(K8S_MEMORY) --keep-context=true || exit 1; \
	fi
	@echo "waiting for Calico"
	@$(KUBECTL) -n kube-system rollout status daemonset/calico-node --timeout=300s
	@$(KUBECTL) -n kube-system rollout status deployment/calico-kube-controllers --timeout=300s

# "minikube image load" keeps the node's copy when the tag already exists, so a
# rebuilt image under the same tag is silently ignored. The build time (Created)
# is the same on the host and in the node, so a difference means the node's copy
# is stale. The old copy is kept under a spare tag first, because removing the
# only name of an image that a container is using fails. The workloads that use
# a replaced image are restarted so they pick up the new one.
k8s-images: ## Load the stack's images into the profile, replacing any the node has stale
	@changed=""; \
	for image in $(K8S_PROJECT_IMAGES) $(K8S_PUBLIC_IMAGES); do \
		if ! docker image inspect "$${image}" >/dev/null 2>&1; then \
			echo "  $${image}: not cached on the host, the node will pull it"; \
			continue; \
		fi; \
		host_built=$$(docker image inspect -f '{{.Created}}' "$${image}"); \
		node_built=$$(minikube -p $(K8S_PROFILE) ssh -- docker image inspect -f '{{.Created}}' "$${image}" 2>/dev/null | tr -d '\r' | grep -E '^[0-9]{4}-' || true); \
		if [ "$${host_built}" = "$${node_built}" ]; then \
			echo "  $${image}: up to date"; \
			continue; \
		fi; \
		if [ -n "$${node_built}" ]; then \
			echo "  replacing stale $${image}"; \
			minikube -p $(K8S_PROFILE) ssh -- "docker tag $${image} $${image}-replaced && docker rmi $${image}" >/dev/null || exit 1; \
			changed="$${changed} $${image}"; \
		else \
			echo "  loading $${image}"; \
		fi; \
		minikube -p $(K8S_PROFILE) image load "$${image}" || exit 1; \
	done; \
	if [ -z "$${changed}" ] || ! $(KUBECTL) get namespace $(K8S_NAMESPACE) >/dev/null 2>&1; then \
		exit 0; \
	fi; \
	for image in $${changed}; do \
		$(KUBECTL) -n $(K8S_NAMESPACE) get deployment,statefulset -o json \
			| jq -r --arg image "$${image}" '.items[] | select(any(.spec.template.spec.containers[]; .image == $$image)) | "\(.kind | ascii_downcase)/\(.metadata.name)"' \
			| xargs -r $(KUBECTL) -n $(K8S_NAMESPACE) rollout restart; \
	done

k8s-config: ## Create the namespace and the ConfigMaps from gateway/envoy/, gateway/opa/ and gateway/keycloak/
	@echo "namespace $(K8S_NAMESPACE) and ConfigMaps from the repo files"
	@$(KUBECTL) create namespace $(K8S_NAMESPACE) --dry-run=client -o yaml | $(KUBECTL) apply -f -
	@$(KUBECTL) -n $(K8S_NAMESPACE) create configmap $(K8S_CM_ENVOY) --from-file=envoy.yaml=gateway/envoy/envoy.yaml --dry-run=client -o yaml | $(KUBECTL) apply -f -
	@$(KUBECTL) -n $(K8S_NAMESPACE) create configmap $(K8S_CM_EGRESS) --from-file=envoy.yaml=gateway/envoy/envoy-egress.yaml --dry-run=client -o yaml | $(KUBECTL) apply -f -
	@$(KUBECTL) -n $(K8S_NAMESPACE) create configmap $(K8S_CM_OPA) --from-file=config.yaml=gateway/opa/config.yaml --dry-run=client -o yaml | $(KUBECTL) apply -f -
	@$(KUBECTL) -n $(K8S_NAMESPACE) create configmap $(K8S_CM_POLICY) --from-file=gateway/opa/policy --dry-run=client -o yaml | $(KUBECTL) apply -f -
	@$(KUBECTL) -n $(K8S_NAMESPACE) create configmap $(K8S_CM_REALM) --from-file=realm-mnemonic.json=gateway/keycloak/realm-mnemonic.json --dry-run=client -o yaml | $(KUBECTL) apply -f -

# The key travels only through a pipe: printf is a shell builtin, so it never
# appears in a process list, and server-side apply keeps it out of the
# last-applied annotation. Recipes are silent so make never echoes them.
k8s-secret: ## Store the OpenAI key (host env MNEMONIC_OPENAI_API_EMBEDDINGS_KEY) in a Secret
	@key="$${MNEMONIC_OPENAI_API_EMBEDDINGS_KEY:-}"; \
	if [ -z "$${key}" ]; then \
		echo "ERROR: set MNEMONIC_OPENAI_API_EMBEDDINGS_KEY to the OpenAI API key" >&2; \
		exit 1; \
	fi; \
	echo "Secret $(K8S_OPENAI_SECRET) from MNEMONIC_OPENAI_API_EMBEDDINGS_KEY"; \
	printf '%s' "$${key}" \
		| $(KUBECTL) -n $(K8S_NAMESPACE) create secret generic $(K8S_OPENAI_SECRET) --from-file=api-key=/dev/stdin --dry-run=client -o yaml \
		| $(KUBECTL) apply --server-side --field-manager=mnemonic-make -f - >/dev/null

k8s-up: k8s-cluster k8s-images k8s-config k8s-secret ## Cluster, images, ConfigMaps, Secret, then install the chart and wait
	@echo "helm upgrade --install $(K8S_RELEASE) (waits up to $(K8S_TIMEOUT))"
	@$(HELM) upgrade --install $(K8S_RELEASE) $(K8S_CHART) --namespace $(K8S_NAMESPACE) \
		--set-string configMaps.envoy=$(K8S_CM_ENVOY) \
		--set-string configMaps.egress=$(K8S_CM_EGRESS) \
		--set-string configMaps.opaConfig=$(K8S_CM_OPA) \
		--set-string configMaps.opaPolicy=$(K8S_CM_POLICY) \
		--set-string configMaps.keycloakRealm=$(K8S_CM_REALM) \
		--set-string openai.existingSecret=$(K8S_OPENAI_SECRET) \
		--wait --timeout $(K8S_TIMEOUT) $(K8S_HELM_ARGS)
	@$(KUBECTL) -n $(K8S_NAMESPACE) get pods

define k8s_forward_start
	@if [ -f $(K8S_RUN_DIR)/$(1).pid ] && kill -0 "$$(cat $(K8S_RUN_DIR)/$(1).pid)" 2>/dev/null; then \
		echo "  $(1): already forwarding (pid $$(cat $(K8S_RUN_DIR)/$(1).pid))"; \
	else \
		nohup $(KUBECTL) -n $(K8S_NAMESPACE) port-forward svc/$(2) $(3) >$(K8S_RUN_DIR)/$(1).log 2>&1 & \
		echo $$! >$(K8S_RUN_DIR)/$(1).pid; \
		echo "  $(1): svc/$(2) $(3) (pid $$!, log $(K8S_RUN_DIR)/$(1).log)"; \
	fi
endef

k8s-forward: ## Port-forward Envoy to localhost:8080/8081 and Keycloak to :8180 (background)
	@mkdir -p $(K8S_RUN_DIR)
	$(call k8s_forward_start,envoy,envoy,8080:8080 8081:8081)
	$(call k8s_forward_start,keycloak,keycloak,8180:8080)
	@for attempt in $$(seq 1 20); do \
		if curl -fs -o /dev/null localhost:8080/version && curl -fs -o /dev/null localhost:8180/realms/mnemonic; then \
			echo "  gateway on localhost:8080 and :8081, Keycloak on localhost:8180"; exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "ERROR: port-forwards did not come up; see $(K8S_RUN_DIR)/*.log" >&2; exit 1

k8s-forward-stop: ## Stop the background port-forwards
	@for name in envoy keycloak; do \
		pid_file="$(K8S_RUN_DIR)/$${name}.pid"; \
		if [ -f "$${pid_file}" ] && kill "$$(cat "$${pid_file}")" 2>/dev/null; then echo "  stopped $${name} port-forward"; fi; \
		rm -f "$${pid_file}"; \
	done

k8s-test: k8s-forward ## Run the end-to-end and isolation checks against the cluster (real OpenAI)
	MNEMONIC_KUBE_CONTEXT=$(K8S_PROFILE) MNEMONIC_NAMESPACE=$(K8S_NAMESPACE) MNEMONIC_RELEASE=$(K8S_RELEASE) deploy/test/k8s-e2e.sh

k8s-down: k8s-forward-stop ## Uninstall the chart and delete the namespace (keeps the cluster)
	-$(HELM) uninstall $(K8S_RELEASE) --namespace $(K8S_NAMESPACE) --wait
	$(KUBECTL) delete namespace $(K8S_NAMESPACE) --ignore-not-found --wait

k8s-delete: k8s-forward-stop ## Delete the whole mnemonic minikube profile
	minikube delete -p $(K8S_PROFILE)
