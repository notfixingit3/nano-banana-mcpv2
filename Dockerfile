# Stage 1: Build the statically linked Go binary
FROM golang:1.22-alpine AS builder

# Set the working directory
WORKDIR /app

# Copy the go.mod file
COPY go.mod ./

# Copy the source code
COPY main.go ./

# Compile the binary with static linking and disabled optimizations for smaller size
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o nano-banana-mcpv2 main.go

# Stage 2: Run in a minimal alpine container
FROM alpine:3.19
RUN apk --no-cache add ca-certificates

# Copy the binary from the builder stage
COPY --from=builder /app/nano-banana-mcpv2 /usr/local/bin/nano-banana-mcpv2

# Expose ENTRYPOINT
ENTRYPOINT ["/usr/local/bin/nano-banana-mcpv2"]
