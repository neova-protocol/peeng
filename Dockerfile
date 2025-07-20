# Build stage

FROM golang:1.24-alpine AS builder
WORKDIR /app
# Install build dependencies for go-sqlite3
RUN apk add --no-cache gcc musl-dev sqlite-dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Enable CGO for go-sqlite3
ENV CGO_ENABLED=1
RUN go build -o peeng main.go

# Runtime stage
FROM alpine:3.19
WORKDIR /app
RUN apk add --no-cache sqlite-libs ca-certificates
COPY --from=builder /app/peeng ./
COPY --from=builder /app/README.md ./
EXPOSE 8080
CMD ["./peeng"]
