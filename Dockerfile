# Use the official Go image as the base image
FROM golang:1.22-alpine

# Install curl for health checks
RUN apk add --no-cache curl tzdata

# Set timezone
ENV TZ=UTC

# Set the working directory
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the Go app with version information
RUN go build -ldflags="-s -w" -o main .

# Expose the port the app runs on
EXPOSE 3000

# Set environment variables
ENV GIN_MODE=release \
    PORT=3000 \
    LOG_LEVEL=debug

# Command to run the app
CMD ["./main"]
