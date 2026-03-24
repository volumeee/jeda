# Step 1: Build the Go Binaries
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install git for any dependencies that may require it
RUN apk add --no-cache git tzdata

# Copy dependency graphs
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire source code
COPY . .

# Build the API and Worker binaries statically for alpine compat
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o bin/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o bin/worker ./cmd/worker


# Step 2: Create a minimal runtime image
FROM alpine:latest

WORKDIR /app

# Install timezone data so cron scheduling works correctly
RUN apk add --no-cache tzdata ca-certificates

# Copy the compiled binaries from the builder stage
COPY --from=builder /app/bin/api .
COPY --from=builder /app/bin/worker .

# Create a small script to start both processes
RUN echo '#!/bin/sh' > /app/start.sh && \
    echo 'set -e' >> /app/start.sh && \
    echo 'echo "🚀 Starting Jeda Worker..."' >> /app/start.sh && \
    echo './worker &' >> /app/start.sh && \
    echo 'WORKER_PID=$!' >> /app/start.sh && \
    echo 'trap "echo -e '\nShutting down Jeda Worker...'; kill $WORKER_PID; exit 0" TERM INT' >> /app/start.sh && \
    echo 'echo "🚀 Starting Jeda API Server..."' >> /app/start.sh && \
    echo './api &' >> /app/start.sh && \
    echo 'API_PID=$!' >> /app/start.sh && \
    echo 'wait $API_PID' >> /app/start.sh

RUN chmod +x /app/start.sh

# Expose the dashboard/API port
EXPOSE 3001

# Run the wrapper script that starts both worker and api
CMD ["./start.sh"]
