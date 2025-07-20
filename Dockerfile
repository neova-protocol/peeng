# Build stage

FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o peeng main.go

# Runtime stage
FROM alpine:3.19
WORKDIR /app
RUN apk add --no-cache sqlite-libs ca-certificates
COPY --from=builder /app/peeng ./
COPY --from=builder /app/README.md ./
EXPOSE 8080
CMD ["./peeng"]
