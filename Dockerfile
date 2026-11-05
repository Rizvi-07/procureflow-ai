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
