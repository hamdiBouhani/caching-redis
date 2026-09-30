# ---- build ----
FROM golang:1.23-alpine AS build

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_TIME=unknown

WORKDIR /src

# Cache deps
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /out/app .

# ---- runtime ----
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/app /app

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/app"]