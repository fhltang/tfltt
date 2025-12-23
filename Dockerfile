# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
# CGO_ENABLED=0 is important for static binaries on Alpine
RUN CGO_ENABLED=0 GOOS=linux go build -o tfltt main.go timetable_renderer.go

# Run stage
FROM alpine:latest

# Install CA certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder /app/tfltt .

# Expose port 8080 (Cloud Run default)
EXPOSE 8080

# Run the binary
CMD ["./tfltt"]
