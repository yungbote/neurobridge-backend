# Build stage
FROM golang:1.24-alpine AS build

RUN apk add --no-cache git ca-certificates && update-ca-certificates
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

# Build the backend binary
RUN CGO_ENABLED=0 \
    go build -ldflags="-s -w" -o /src/backend ./cmd

# Runtime stage
FROM gcr.io/distroless/base-debian12

WORKDIR /app

COPY --from=build /src/backend /app/backend
COPY --from=build /src/fonts /app/fonts
COPY --from=build /src/json /app/json

EXPOSE 8080
ENTRYPOINT ["/app/backend"]










