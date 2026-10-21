#------------------------------------------------------------
# Stage 1: Run linters, formatters, security scanners, 
# unit tests, then build binary
#------------------------------------------------------------
ARG BUILDPLATFORM
FROM --platform=${BUILDPLATFORM} ghcr.io/twistingmercury/golang-tooling:go1.27.3 AS build

ARG TARGETOS=linux
ARG TARGETARCH=amd64

ENV CGO_ENABLED=0

ARG BUILD_VER=dev
ARG BUILD_DATE=unknown
ARG BUILD_COMMIT=unknown

WORKDIR /src

COPY src/go.mod src/go.sum ./
RUN go mod download
COPY src/.golangci.yml ./
COPY src/cmd/api ./cmd/api
COPY src/internal/api ./internal/api
COPY src/docs/api ./docs/api

# Generate Swagger 2.0 docs (required before linters/build — routes.go imports the generated package)
RUN go install github.com/swaggo/swag/cmd/swag@v1.16.6
RUN swag init -g cmd/api/main.go -o docs/api/swagger --parseInternal

# Run linters, formatters, security scanners, etc
RUN goimports -w .
RUN golangci-lint run
RUN govulncheck ./cmd/... ./internal/...
RUN gosec -quiet -exclude-dir=tests ./...
RUN go test -v ./cmd/api/... ./internal/... -coverprofile=coverage.out

ARG LD_FLAGS="-s -w \
    -X 'github.com/twistingmercury/mnemonic/internal/api/version.version=${BUILD_VER}' \
    -X 'github.com/twistingmercury/mnemonic/internal/api/version.buildDate=${BUILD_DATE}' \
    -X 'github.com/twistingmercury/mnemonic/internal/api/version.commit=${BUILD_COMMIT}'"

RUN GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build --ldflags "${LD_FLAGS}" -o /out/mnemonic-api ./cmd/api


#------------------------------------------------------------
# Stage 2: Runtime - minimal production image
#------------------------------------------------------------
FROM scratch AS final

ARG BUILD_VER=dev
ARG BUILD_DATE=unknown
ARG BUILD_COMMIT=unknown

LABEL org.opencontainers.image.title="mnemonic-api" \
    org.opencontainers.image.description="Admin REST API for reusable knowledge-graph patterns" \
    org.opencontainers.image.source="https://github.com/twistingmercury/mnemonic" \
    org.opencontainers.image.licenses="Apache-2.0" \
    org.opencontainers.image.authors="Jeremy K. Johnson" \
    org.opencontainers.image.created="${BUILD_DATE}" \
    org.opencontainers.image.revision="${BUILD_COMMIT}" \
    org.opencontainers.image.version="${BUILD_VER}"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/mnemonic-api /mnemonic-api
COPY LICENSE /licenses/LICENSE

USER 65532:65532

EXPOSE 8080 9090

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/mnemonic-api", "--health"]

ENTRYPOINT ["/mnemonic-api"]
