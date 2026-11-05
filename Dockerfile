# syntax=docker/dockerfile:1
FROM golang:1.27-alpine3.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY gen ./gen
COPY migrations ./migrations
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/purchase-service ./cmd/purchase-service && \
    go build -trimpath -ldflags="-s -w" -o /out/approval-service ./cmd/approval-service && \
    go build -trimpath -ldflags="-s -w" -o /out/api-gateway ./cmd/api-gateway && \
    go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    go build -trimpath -ldflags="-s -w" -o /out/assistant-service ./cmd/assistant-service && \
    go build -trimpath -ldflags="-s -w" -o /out/policy-import ./cmd/policy-import && \
    go build -trimpath -ldflags="-s -w" -o /out/auth-seed ./cmd/auth-seed && \
    go build -trimpath -ldflags="-s -w" -o /out/auth-setup ./cmd/auth-setup && \
    go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM alpine:3.23 AS runtime
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/healthcheck /usr/local/bin/healthcheck
USER 10001:10001

FROM runtime AS purchase-service
COPY --from=build /out/purchase-service /app/purchase-service
EXPOSE 50051
ENTRYPOINT ["/app/purchase-service"]

FROM runtime AS approval-service
COPY --from=build /out/approval-service /app/approval-service
EXPOSE 50052
ENTRYPOINT ["/app/approval-service"]

FROM runtime AS api-gateway
COPY --from=build /out/api-gateway /app/api-gateway
EXPOSE 8080
ENTRYPOINT ["/app/api-gateway"]

FROM runtime AS migrate
COPY --from=build /out/migrate /app/migrate
ENTRYPOINT ["/app/migrate"]

FROM runtime AS assistant-service
COPY --from=build /out/assistant-service /app/assistant-service
EXPOSE 50053
ENTRYPOINT ["/app/assistant-service"]

FROM runtime AS policy-import
COPY --from=build /out/policy-import /app/policy-import
COPY policies /policies
ENV POLICY_DIR=/policies
ENTRYPOINT ["/app/policy-import"]

# One-time local credential provisioning, with a host-mounted /workspace/secrets.
FROM runtime AS auth-setup
USER root
WORKDIR /workspace
COPY --from=build /out/auth-setup /app/auth-setup
ENTRYPOINT ["/app/auth-setup"]

FROM runtime AS auth-seed
COPY --from=build /out/auth-seed /app/auth-seed
ENTRYPOINT ["/app/auth-seed", "-users-file", "/run/secrets/auth-users.json", "-admin-file", "/run/secrets/admin-account.json"]
