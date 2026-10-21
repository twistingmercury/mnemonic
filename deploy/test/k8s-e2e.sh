#!/usr/bin/env bash
# k8s-e2e.sh: end-to-end and isolation checks for the Mnemonic chart on the
# mnemonic minikube profile. Talks to the gateway through the port-forwards
# from `make k8s-forward`, enriches with the real OpenAI API through the egress
# proxy, and proves the NetworkPolicies are enforced with short-lived probe
# pods. Every isolation check has a positive control, so a probe that cannot
# reach anything never passes as "blocked".
#
# Usage: make k8s-test, or MNEMONIC_KUBE_CONTEXT=mnemonic deploy/test/k8s-e2e.sh

set -euo pipefail

MNEMONIC_KUBE_CONTEXT="${MNEMONIC_KUBE_CONTEXT:-mnemonic}"
MNEMONIC_NAMESPACE="${MNEMONIC_NAMESPACE:-mnemonic}"
MNEMONIC_RELEASE="${MNEMONIC_RELEASE:-mnemonic}"
MNEMONIC_API_URL="${MNEMONIC_API_URL:-http://localhost:8080}"
MNEMONIC_MCP_URL="${MNEMONIC_MCP_URL:-http://localhost:8081}"
MNEMONIC_KEYCLOAK_URL="${MNEMONIC_KEYCLOAK_URL:-http://localhost:8180}"
MNEMONIC_PROBE_IMAGE="${MNEMONIC_PROBE_IMAGE:-curlimages/curl:8.10.1}"
MNEMONIC_OPENAI_SECRET="${MNEMONIC_OPENAI_SECRET:-mnemonic-openai}"
MNEMONIC_ENRICH_ATTEMPTS="${MNEMONIC_ENRICH_ATTEMPTS:-45}"
MNEMONIC_EXPECTED_PODS="${MNEMONIC_EXPECTED_PODS:-10}"

readonly MCP_PROTOCOL="2026-07-28"
readonly PROBE_LABEL="mnemonic-e2e=probe"
readonly TEST_PATTERNS="retry-with-backoff circuit-breaker table-driven-tests retry-with-jitter"
readonly INTERNET_URL="https://api.openai.com/v1/models"

# Run inside the pods, so they live in quoted heredocs and this shell never
# expands them.
PROBE_SCRIPT=$(cat <<'EOF_PROBE'
for url in "$@"; do
    code=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 -m 12 "${url}")
    rc=$?
    printf "%s %s %s\n" "${url}" "${code}" "${rc}"
done
EOF_PROBE
)
readonly PROBE_SCRIPT
# The Neo4j password comes from the pod's own NEO4J_AUTH, not from here.
CYPHER_SCRIPT=$(cat <<'EOF_CYPHER'
NEO4J_PASSWORD="${NEO4J_AUTH#*/}" cypher-shell -u "${NEO4J_AUTH%%/*}" --format plain "$1"
EOF_CYPHER
)
readonly CYPHER_SCRIPT

PASS_COUNT=0
FAIL_COUNT=0
PROBE_SEQ=0

# ------------------------------------------------------------------ reporting

record_pass() {
    local name="${1}"

    printf "  PASS  %s\n" "${name}"
    PASS_COUNT=$((PASS_COUNT + 1))

    return 0
}

record_fail() {
    local name="${1}"

    printf "  FAIL  %s\n" "${name}"
    FAIL_COUNT=$((FAIL_COUNT + 1))

    return 0
}

check_equal() {
    local name="${1}"
    local got="${2}"
    local want="${3}"

    if [ "${got}" = "${want}" ]; then
        record_pass "${name}"
        return 0
    fi

    record_fail "${name} (got '${got}', want '${want}')"

    return 0
}

check_positive() {
    local name="${1}"
    local count="${2}"

    if [ "${count:-0}" -gt 0 ] 2>/dev/null; then
        record_pass "${name} (${count})"
        return 0
    fi

    record_fail "${name} (got '${count}')"

    return 0
}

section() {
    printf "== %s\n" "${1}"
    return 0
}

# ------------------------------------------------------------------ helpers

kube() {
    kubectl --context "${MNEMONIC_KUBE_CONTEXT}" -n "${MNEMONIC_NAMESPACE}" "$@"
}

validate_args() {
    local tool

    for tool in kubectl curl jq; do
        if ! command -v "${tool}" >/dev/null 2>&1; then
            printf "ERROR: %s is required\n" "${tool}" >&2
            return 1
        fi

    done

    if ! kubectl config get-contexts "${MNEMONIC_KUBE_CONTEXT}" >/dev/null 2>&1; then
        printf "ERROR: kubectl context %s not found; run make k8s-up\n" "${MNEMONIC_KUBE_CONTEXT}" >&2
        return 1
    fi

    if ! curl -fs -m 5 -o /dev/null "${MNEMONIC_API_URL}/version"; then
        printf "ERROR: gateway not reachable at %s; run make k8s-forward\n" "${MNEMONIC_API_URL}" >&2
        return 1
    fi

    return 0
}

http_code() {
    curl -s -m 20 -o /dev/null -w '%{http_code}' "$@" || true
}

token() {
    local client="${1}"

    curl -s -m 10 "${MNEMONIC_KEYCLOAK_URL}/realms/mnemonic/protocol/openid-connect/token" \
        -d grant_type=client_credentials -d client_id="${client}" -d client_secret="${client}-secret" \
        | jq -r .access_token
}

# Calls an MCP tool through the gateway and prints the first text result.
mcp_call() {
    local tool="${1}"
    local arguments="${2}"
    local body

    body=$(jq -cn --arg tool "${tool}" --argjson args "${arguments}" --arg version "${MCP_PROTOCOL}" \
        '{jsonrpc:"2.0",id:1,method:"tools/call",params:{
            _meta:{"io.modelcontextprotocol/protocolVersion":$version,
                   "io.modelcontextprotocol/clientInfo":{name:"k8s-e2e",version:"0"},
                   "io.modelcontextprotocol/clientCapabilities":{}},
            name:$tool,arguments:$args}}')

    curl -s -m 40 -X POST "${MNEMONIC_MCP_URL}/mcp" -H "Authorization: Bearer $(token mcp-agent)" \
        -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
        -H "MCP-Protocol-Version: ${MCP_PROTOCOL}" -H 'Mcp-Method: tools/call' -H "Mcp-Name: ${tool}" \
        -d "${body}" \
        | sed 's/^data: //' | grep -v '^event:' | jq -r '.result.content[0].text // (.error|tostring)' || true
}

post_pattern() {
    local name="${1}"
    local domain="${2}"
    local description="${3}"
    local tags="${4}"
    local content="${5}"

    jq -n --arg n "${name}" --arg d "${domain}" --arg desc "${description}" --argjson t "${tags}" --arg c "${content}" \
        '{name:$n,entity_type:"coding-pattern",language:"go",domain:$d,description:$desc,tags:$t,content:$c}' \
        | http_code -X POST "${MNEMONIC_API_URL}/v1/api/patterns" -H "Authorization: Bearer $(token api-writer)" \
            -H 'Content-Type: application/json' -d @-
}

# Prints "name<TAB>id<TAB>status" for every test pattern that exists.
list_test_patterns() {
    local names_json

    names_json=$(jq -cn --arg names "${TEST_PATTERNS} bad-one" '$names | split(" ")')
    curl -s -m 20 -H "Authorization: Bearer $(token api-reader)" "${MNEMONIC_API_URL}/v1/api/patterns?limit=100" \
        | jq -r --argjson names "${names_json}" \
            '.data[] | select(.name as $n | $names | index($n)) | [.name, .id, .enrichment_status] | @tsv'
}

delete_test_patterns() {
    local pattern_id
    local admin_token

    admin_token=$(token api-admin)
    for pattern_id in $(list_test_patterns | cut -f2); do
        curl -s -m 20 -o /dev/null -X DELETE -H "Authorization: Bearer ${admin_token}" \
            "${MNEMONIC_API_URL}/v1/api/patterns/${pattern_id}" || true
    done

    return 0
}

cypher() {
    local query="${1}"

    kube exec neo4j-0 -c neo4j -- sh -c "${CYPHER_SCRIPT}" cypher "${query}" 2>/dev/null | tail -1 || true
}

# Every result row without the header line, for queries that return several rows.
cypher_rows() {
    local query="${1}"

    kube exec neo4j-0 -c neo4j -- sh -c "${CYPHER_SCRIPT}" cypher "${query}" 2>/dev/null | tail -n +2 || true
}

pod_for() {
    local component="${1}"

    kube get pod -l "app.kubernetes.io/instance=${MNEMONIC_RELEASE},app.kubernetes.io/component=${component}" \
        -o jsonpath='{.items[0].metadata.name}'
}

# Names of every Secret a pod references (env, envFrom or volumes).
pod_secret_refs() {
    local pod="${1}"

    kube get pod "${pod}" -o json \
        | jq -r '[.. | objects | (.secretKeyRef.name?, .secretRef.name?, .secretName?) | strings] | unique | .[]'
}

# ------------------------------------------------------------------ probe pods

# Runs one short-lived curl pod with the given labels ("" for none of the
# chart's labels) and prints one "url http_code curl_exit" line per URL.
run_probe() {
    local labels="${1}"
    local pod_name
    local all_labels="${PROBE_LABEL}"

    shift
    PROBE_SEQ=$((PROBE_SEQ + 1))
    pod_name="e2e-probe-$$-${PROBE_SEQ}"

    if [ -n "${labels}" ]; then
        all_labels="${PROBE_LABEL},${labels}"
    fi

    kube run "${pod_name}" --image="${MNEMONIC_PROBE_IMAGE}" --image-pull-policy=IfNotPresent \
        --restart=Never --labels="${all_labels}" --command -- sh -c "${PROBE_SCRIPT}" probe "$@" >/dev/null

    if ! kube wait --for=jsonpath='{.status.phase}'=Succeeded "pod/${pod_name}" --timeout=180s >/dev/null 2>&1; then
        printf "ERROR: probe pod %s did not complete\n" "${pod_name}" >&2
    fi

    kube logs "${pod_name}" 2>/dev/null || true
    kube delete pod "${pod_name}" --wait=false >/dev/null 2>&1 || true

    return 0
}

# Policy labels of a component, as NetworkPolicies match them. They leave out
# app.kubernetes.io/name, so probe pods never join the component's Service.
labels_for() {
    local component="${1}"

    printf "%s" "app.kubernetes.io/instance=${MNEMONIC_RELEASE},app.kubernetes.io/component=${component}"
}

# Classifies one probe line: "blocked" when the TCP connect timed out (curl
# exit 28), "dns-failed" (exit 6) when the probe could not even resolve the
# name, else "reachable" (any reply, a reset or a refused connection).
classify() {
    local results="${1}"
    local url="${2}"
    local line
    local curl_exit

    line=$(printf '%s\n' "${results}" | awk -v url="${url}" '$1 == url { print; exit }')
    if [ -z "${line}" ]; then
        printf "%s" "no-result"
        return 0
    fi

    curl_exit=$(printf '%s' "${line}" | awk '{ print $3 }')
    if [ "${curl_exit}" = "28" ]; then
        printf "%s" "blocked"
        return 0
    fi

    if [ "${curl_exit}" = "6" ]; then
        printf "%s" "dns-failed"
        return 0
    fi

    printf "%s" "reachable"

    return 0
}

http_status_of() {
    local results="${1}"
    local url="${2}"

    printf '%s\n' "${results}" | awk -v url="${url}" '$1 == url { print $2; exit }'
}

expect_flow() {
    local name="${1}"
    local results="${2}"
    local url="${3}"
    local want="${4}"
    local got

    got=$(classify "${results}" "${url}")
    check_equal "${name}" "${got}" "${want}"

    return 0
}

# ------------------------------------------------------------------ checks

check_stack() {
    local ready_count
    local policy

    section "1. stack ready"
    ready_count=$(kube get pods -l "app.kubernetes.io/instance=${MNEMONIC_RELEASE},app.kubernetes.io/name=mnemonic" -o json \
        | jq '[.items[] | select(.status.conditions[]? | .type == "Ready" and .status == "True")] | length')
    check_equal "${MNEMONIC_EXPECTED_PODS} pods Ready" "${ready_count}" "${MNEMONIC_EXPECTED_PODS}"

    policy=$(kube get networkpolicy default-deny-all -o jsonpath='{.spec.policyTypes}' 2>/dev/null || true)
    check_equal "default-deny NetworkPolicy installed" "${policy}" '["Ingress","Egress"]'

    return 0
}

check_auth() {
    section "2. auth at the gateway"
    check_equal "no token -> 401" "$(http_code "${MNEMONIC_API_URL}/v1/api/patterns")" "401"
    check_equal "reader token GET -> 200" \
        "$(http_code -H "Authorization: Bearer $(token api-reader)" "${MNEMONIC_API_URL}/v1/api/patterns")" "200"
    check_equal "reader token POST -> 403" \
        "$(http_code -X POST -H "Authorization: Bearer $(token api-reader)" -H 'Content-Type: application/json' -d '{}' "${MNEMONIC_API_URL}/v1/api/patterns")" "403"
    check_equal "mcp without token -> 401" "$(http_code -X POST "${MNEMONIC_MCP_URL}/mcp")" "401"

    return 0
}

check_ingest() {
    section "3. ingest a mixed corpus (writer token)"
    check_equal "retry pattern accepted" "$(post_pattern retry-with-backoff backend \
        'Retry transient failures with exponential backoff' '["retry","backoff"]' \
        $'# Retry\n\n## Overview\n\nRetry transient failures.\n\n[//]: pattern\n## Pattern\n\nWait longer after each failed attempt, double the delay, and add random jitter so clients do not retry together. Stop after a fixed number of attempts.\n')" "202"
    check_equal "circuit breaker accepted" "$(post_pattern circuit-breaker backend \
        'Stop calling a failing dependency to let it recover' '["resilience","circuit-breaker"]' \
        $'# Circuit Breaker\n\n## Overview\n\nProtect callers from a failing dependency.\n\n[//]: pattern\n## Pattern\n\nAfter repeated failures, open the circuit and fail fast for a cool-down period. Then let a single trial request through to test whether the dependency has recovered.\n')" "202"
    check_equal "table tests accepted" "$(post_pattern table-driven-tests testing \
        'Cover many cases with one test function' '["testing","go"]' \
        $'# Table Driven Tests\n\n## Overview\n\nOne loop, many cases.\n\n[//]: pattern\n## Pattern\n\nList inputs and expected outputs in a slice of structs and loop over them, running each as a named subtest.\n')" "202"
    check_equal "retry jitter accepted" "$(post_pattern retry-with-jitter backend \
        'Add random jitter to retry delays' '["retry","backoff","jitter"]' \
        $'# Retry Jitter\n\n## Overview\n\nSpread out retries.\n\n[//]: pattern\n## Pattern\n\nWhen retrying transient failures with exponential backoff, add random jitter to each delay so many clients do not retry at the same moment.\n')" "202"
    check_equal "bad domain rejected -> 400" "$(post_pattern bad-one nonsense x '[]' \
        $'# X\n\n## Overview\n\nx\n\n[//]: pattern\n## P\n\nx\n')" "400"

    return 0
}

enrichment_states() {
    list_test_patterns | cut -f3 | sort | paste -sd, -
}

check_enrichment() {
    local attempt=1
    local states=""

    section "4. enrichment (real embeddings and extraction through the egress proxy)"
    while [ "${attempt}" -le "${MNEMONIC_ENRICH_ATTEMPTS}" ]; do
        states=$(enrichment_states)
        if [ "${states}" = "enriched,enriched,enriched,enriched" ]; then
            break
        fi

        sleep 2
        attempt=$((attempt + 1))
    done

    check_equal "all 4 enriched" "${states}" "enriched,enriched,enriched,enriched"

    return 0
}

check_graph() {
    section "5. graph (Neo4j, via kubectl exec)"
    check_positive "concepts extracted" "$(cypher 'MATCH (c:Concept) RETURN count(c)')"
    check_positive "MENTIONED_IN edges" "$(cypher 'MATCH ()-[r:MENTIONED_IN]->() RETURN count(r)')"
    check_equal "schema ships concept_name_unique" \
        "$(cypher "SHOW CONSTRAINTS YIELD name WHERE name = 'concept_name_unique' RETURN count(*)")" "1"
    check_equal "no duplicate Concept names" \
        "$(cypher 'MATCH (c:Concept) WITH c.name AS name, count(*) AS copies WHERE copies > 1 RETURN count(name)')" "0"

    return 0
}

check_mcp() {
    local output
    local pattern_id

    section "6. MCP tools through the gateway"
    # The default threshold (0.5) hides natural-language matches.
    output=$(mcp_call search_patterns '{"query":"how do I keep calling a service that keeps timing out without hammering it","threshold":0.3}')
    if printf '%s\n' "${output}" | grep -qE 'retry-with-backoff|circuit-breaker'; then
        record_pass "semantic search finds resilience patterns"
    else
        record_fail "semantic search finds resilience patterns (got: $(printf '%s\n' "${output}" | head -1))"
    fi

    output=$(mcp_call search_patterns '{"query":"loop over test cases in a slice of structs"}')
    if printf '%s\n' "${output}" | head -8 | grep -q 'table-driven-tests'; then
        record_pass "search ranks table-driven-tests first for a testing query"
    else
        record_fail "search ranks table-driven-tests first for a testing query"
    fi

    pattern_id=$(list_test_patterns | awk -F '\t' '$1 == "retry-with-backoff" { print $2 }')
    output=$(mcp_call get_pattern "{\"id\":\"${pattern_id}\"}")
    if printf '%s\n' "${output}" | grep -q 'double the delay'; then
        record_pass "get_pattern returns full content"
    else
        record_fail "get_pattern returns full content"
    fi

    return 0
}

# The minimum similarity the enricher applies; its default is 0.3.
related_threshold() {
    local value

    value=$(kube get deploy -l "app.kubernetes.io/instance=${MNEMONIC_RELEASE},app.kubernetes.io/component=enricher" \
        -o jsonpath='{.items[0].spec.template.spec.containers[0].env[?(@.name=="MNEMONIC_ENRICHMENT_RELATED_TO_MIN_SIMILARITY")].value}')

    printf '%s' "${value:-0.3}"
}

# One "a,b,similarity,edges" row per pair of test patterns. The similarity is
# computed the way the enricher does it: shared concept names divided by the
# larger of the two concept counts.
pair_rows() {
    local patterns
    local names
    local rows

    read -r -a patterns <<< "${TEST_PATTERNS}"
    names=$(printf '"%s",' "${patterns[@]}")
    names="[${names%,}]"
    rows=$(cypher_rows "MATCH (a:Pattern), (b:Pattern) WHERE a.name IN ${names} AND b.name IN ${names} AND a.name < b.name
        OPTIONAL MATCH (ca:Concept)-[:MENTIONED_IN]->(a)
        WITH a, b, collect(DISTINCT ca.name) AS na
        OPTIONAL MATCH (cb:Concept)-[:MENTIONED_IN]->(b)
        WITH a, b, na, collect(DISTINCT cb.name) AS nb
        WITH a, b, size([x IN na WHERE x IN nb]) AS shared, CASE WHEN size(na) > size(nb) THEN size(na) ELSE size(nb) END AS most
        WITH a, b, CASE WHEN most = 0 THEN 0.0 ELSE toFloat(shared) / toFloat(most) END AS similarity
        OPTIONAL MATCH (a)-[r:RELATED_TO]-(b)
        RETURN a.name, b.name, similarity, count(r)")

    printf '%s\n' "${rows}" | tr -d '" '
}

# Concept names vary from run to run, so whether a given pair is related is not
# fixed. What is fixed is the rule: a RELATED_TO edge exists once, and only
# when the similarity reaches the threshold. Each pair is checked against it.
check_relation_rule() {
    local threshold
    local name_a
    local name_b
    local similarity
    local edges
    local want
    local related=0
    local output

    section "6b. RELATED_TO follows the similarity rule"
    threshold=$(related_threshold)

    while IFS=',' read -r name_a name_b similarity edges; do
        if [ -z "${name_a}" ]; then
            continue
        fi

        want=$(awk -v s="${similarity}" -v t="${threshold}" 'BEGIN { print (s + 1e-9 >= t) ? 1 : 0 }')
        check_equal "${name_a} / ${name_b}: similarity ${similarity} -> ${want} edge(s) at threshold ${threshold}" "${edges}" "${want}"
        if [ "${want}" -eq 1 ]; then
            related=$((related + 1))
            output=$(mcp_call find_related_patterns "{\"pattern_id\":\"$(list_test_patterns | awk -F '\t' -v n="${name_a}" '$1 == n { print $2 }')\"}")
            if printf '%s\n' "${output}" | grep -q "${name_b}"; then
                record_pass "find_related from ${name_a} lists ${name_b}"
            else
                record_fail "find_related from ${name_a} lists ${name_b}"
            fi

        fi
    done <<< "$(pair_rows)"

    printf "  NOTE  %d of the pairs reached the threshold this run\n" "${related}"

    return 0
}

check_key_handling() {
    local component
    local pod
    local key_value
    local holders=""
    local egress_ref

    section "7. key handling"
    for component in api mcp enricher; do
        pod=$(pod_for "${component}")
        key_value=$(kube get pod "${pod}" \
            -o jsonpath='{.spec.containers[0].env[?(@.name=="MNEMONIC_OPENAI_API_KEY")].value}')
        check_equal "${component} holds the placeholder key only" "${key_value}" "proxy-managed"
    done

    for pod in $(kube get pods -o jsonpath='{.items[*].metadata.name}'); do
        if pod_secret_refs "${pod}" | grep -qx "${MNEMONIC_OPENAI_SECRET}"; then
            holders="${holders}$(kube get pod "${pod}" -o jsonpath='{.metadata.labels.app\.kubernetes\.io/component}') "
        fi

    done
    check_equal "only the egress pod references Secret ${MNEMONIC_OPENAI_SECRET}" "${holders% }" "egress"

    egress_ref=$(kube get pod "$(pod_for egress)" \
        -o jsonpath='{.spec.containers[0].env[?(@.name=="MNEMONIC_OPENAI_API_EMBEDDINGS_KEY")].valueFrom.secretKeyRef.name}')
    check_equal "egress reads the key from the Secret" "${egress_ref}" "${MNEMONIC_OPENAI_SECRET}"

    return 0
}

check_egress_proxy() {
    local results
    local models_url="http://egress:8443/v1/models"
    local embeddings_url="http://egress:8443/v1/embeddings"

    section "8. egress proxy refusals (from an enricher-labelled probe)"
    results=$(run_probe "$(labels_for enricher)" "${models_url}" "${embeddings_url}")
    check_equal "egress refuses /v1/models" "$(http_status_of "${results}" "${models_url}")" "403"
    check_equal "egress refuses GET on embeddings" "$(http_status_of "${results}" "${embeddings_url}")" "403"

    # A POST needs its own pod: run_probe only issues GETs.
    results=$(kube run "e2e-probe-$$-post" --image="${MNEMONIC_PROBE_IMAGE}" --image-pull-policy=IfNotPresent \
        --restart=Never --labels="${PROBE_LABEL},$(labels_for enricher)" --rm -i --quiet --command -- \
        curl -s -o /dev/null -w '%{http_code}' -m 30 -X POST "${embeddings_url}" \
        -H 'Authorization: Bearer sk-bogus' -H 'Content-Type: application/json' \
        -d '{"model":"text-embedding-3-large","input":"x","dimensions":8}' 2>/dev/null || true)
    check_equal "egress overwrites a bogus caller key (real OpenAI call)" "${results}" "200"

    return 0
}

check_isolation_unlabelled() {
    local results

    section "9b. a pod without the chart's labels reaches no service"
    results=$(run_probe "" http://envoy:8080/version http://postgres:5432/ http://neo4j:7687/ \
        http://rabbitmq:5672/ http://mnemonic-api:8080/version http://mnemonic-mcp:8081/ \
        http://egress:8443/v1/embeddings http://opa:9191/ http://keycloak:8080/)
    # No positive control here: namespace egress is default-deny, so a pod with
    # no chart labels is not allowed to reach anything, the envoy edge included
    # (outside clients arrive through the port-forward, not from a pod). "blocked"
    # already means DNS resolved and the connection timed out, so the probe itself
    # works; the positive controls are the labelled pods in 9c.
    expect_flow "unlabelled -> envoy edge blocked" "${results}" http://envoy:8080/version blocked
    expect_flow "unlabelled -> postgres blocked" "${results}" http://postgres:5432/ blocked
    expect_flow "unlabelled -> neo4j blocked" "${results}" http://neo4j:7687/ blocked
    expect_flow "unlabelled -> rabbitmq blocked" "${results}" http://rabbitmq:5672/ blocked
    expect_flow "unlabelled -> mnemonic-api blocked" "${results}" http://mnemonic-api:8080/version blocked
    expect_flow "unlabelled -> mnemonic-mcp blocked" "${results}" http://mnemonic-mcp:8081/ blocked
    expect_flow "unlabelled -> egress proxy blocked" "${results}" http://egress:8443/v1/embeddings blocked
    expect_flow "unlabelled -> opa blocked" "${results}" http://opa:9191/ blocked
    expect_flow "unlabelled -> keycloak blocked" "${results}" http://keycloak:8080/ blocked

    return 0
}

check_isolation_services() {
    local results

    section "9c. the services reach only their own dependencies"
    results=$(run_probe "$(labels_for api)" http://postgres:5432/ http://rabbitmq:5672/ \
        http://mnemonic-mcp:8081/ http://mnemonic-enricher:8080/health)
    expect_flow "control: api-labelled pod reaches postgres" "${results}" http://postgres:5432/ reachable
    expect_flow "control: api-labelled pod reaches rabbitmq" "${results}" http://rabbitmq:5672/ reachable
    expect_flow "api -> mnemonic-mcp blocked" "${results}" http://mnemonic-mcp:8081/ blocked
    expect_flow "api -> mnemonic-enricher blocked" "${results}" http://mnemonic-enricher:8080/health blocked

    results=$(run_probe "$(labels_for mcp)" http://neo4j:7687/ http://mnemonic-api:8080/version \
        http://mnemonic-enricher:8080/health http://rabbitmq:5672/)
    expect_flow "control: mcp-labelled pod reaches neo4j" "${results}" http://neo4j:7687/ reachable
    expect_flow "mcp -> mnemonic-api blocked" "${results}" http://mnemonic-api:8080/version blocked
    expect_flow "mcp -> mnemonic-enricher blocked" "${results}" http://mnemonic-enricher:8080/health blocked
    expect_flow "mcp -> rabbitmq blocked (MCP is read-only)" "${results}" http://rabbitmq:5672/ blocked

    results=$(run_probe "$(labels_for enricher)" http://egress:8443/v1/models http://mnemonic-api:8080/version \
        http://mnemonic-mcp:8081/)
    expect_flow "control: enricher-labelled pod reaches the egress proxy" "${results}" http://egress:8443/v1/models reachable
    expect_flow "enricher -> mnemonic-api blocked" "${results}" http://mnemonic-api:8080/version blocked
    expect_flow "enricher -> mnemonic-mcp blocked" "${results}" http://mnemonic-mcp:8081/ blocked

    return 0
}

check_internet_from() {
    local component="${1}"
    local want="${2}"
    local labels=""
    local results

    if [ "${component}" != "unlabelled" ]; then
        labels=$(labels_for "${component}")
    fi

    results=$(run_probe "${labels}" "${INTERNET_URL}")
    expect_flow "${component} -> ${INTERNET_URL} ${want}" "${results}" "${INTERNET_URL}" "${want}"

    return 0
}

check_isolation_internet() {
    local component

    section "9a/9d. only the egress pod reaches the internet"
    check_internet_from egress reachable
    for component in enricher api mcp envoy opa keycloak postgres neo4j rabbitmq unlabelled; do
        check_internet_from "${component}" blocked
    done

    return 0
}

check_cleanup() {
    local remaining

    section "10. cleanup of test data (admin token)"
    delete_test_patterns
    remaining=$(list_test_patterns | wc -l | tr -d ' ')
    check_equal "test corpus deleted" "${remaining}" "0"

    return 0
}

cleanup() {
    kube delete pod -l "${PROBE_LABEL}" --wait=false >/dev/null 2>&1 || true
    return 0
}

main() {
    if ! validate_args; then
        return 1
    fi

    trap cleanup EXIT

    # Leftovers from an interrupted run would skew the enrichment counts.
    delete_test_patterns

    check_stack
    check_auth
    check_ingest
    check_enrichment
    check_graph
    check_mcp
    check_relation_rule
    check_key_handling
    check_egress_proxy
    check_isolation_internet
    check_isolation_unlabelled
    check_isolation_services
    check_cleanup

    printf "\nRESULT: %d passed, %d failed\n" "${PASS_COUNT}" "${FAIL_COUNT}"
    if [ "${FAIL_COUNT}" -gt 0 ]; then
        return 1
    fi

    return 0
}

main "$@"
