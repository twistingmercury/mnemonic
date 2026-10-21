OPA_VERSION   ?= 1.4.2
ENVOY_IMAGE   ?= envoyproxy/envoy:v1.34.1
COMPOSE_FILE  := deploy/docker-compose.yaml
STUB_COMPOSE  := docker compose -f gateway/test/compose.stub.yaml

.PHONY: help test-policy validate-envoy up down e2e e2e-stub

help: ## Show targets
	@grep -E '^[a-z0-9-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

test-policy: ## Format-check, type-check and unit-test the OPA policies
	opa fmt --list --fail gateway/opa/policy
	opa check --strict gateway/opa/policy
	opa test -v gateway/opa/policy

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
