# Use the official Go image as the base image
FROM golang:1.22-alpine

# Install Redis
RUN apk add --no-cache redis

# Set the working directory
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the Go app
RUN go build -o main .

# Expose the port the app runs on
EXPOSE 8080

# Create a script to start both Redis and the Go application
RUN echo '#!/bin/sh\nredis-server --daemonize yes && ./main' > /app/start.sh && chmod +x /app/start.sh

# Command to run the script
CMD ["/app/start.sh"]
