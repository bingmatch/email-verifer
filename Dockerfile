# Use the official Go image as the base image
FROM golang:1.22-alpine

# Install curl for health checks
RUN apk add --no-cache curl

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

# Command to run the app
CMD ["./main"]
