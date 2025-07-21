# Build stage
FROM golang:1.24-alpine AS builder
WORKDIR /app

# Install build dependencies for go-sqlite3
# Make sure sqlite-dev is installed as it's needed for building the go-sqlite3 driver
RUN apk add --no-cache gcc musl-dev sqlite-dev

# Copy go.mod and go.sum first to leverage Docker layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application source code
COPY . .

# Enable CGO for go-sqlite3 and build the executable for Linux
# Use -ldflags="-s -w -extldflags '-static'" for smaller, statically linked binaries
# This is crucial for Alpine images to avoid glibc dependencies.
ENV CGO_ENABLED=1
ENV GOOS=linux
ENV GOARCH=amd64
RUN go build -o peeng -ldflags="-s -w -extldflags '-static'" main.go

# Runtime stage
FROM alpine:3.19
WORKDIR /app

# Install necessary runtime libraries for SQLite and certificates
# sqlite-libs for runtime, ca-certificates for HTTPS calls
RUN apk add --no-cache sqlite-libs ca-certificates

# Copy the built executable from the builder stage
# Explicitly copy it to a named path within /app for clarity and robustness
COPY --from=builder /app/peeng /app/peeng

# Remove the README.md copy, it's not needed for execution
# COPY --from=builder /app/README.md ./

EXPOSE 8080
CMD ["./peeng"]