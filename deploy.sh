#!/bin/bash

# Stop and remove existing containers if they exist
docker stop email-verifier || true
docker rm email-verifier || true

# Build the new image
docker build -t email-verifier .

# Run Redis and the application
docker run -d \
    --name email-verifier \
    -p 8080:8080 \
    --restart unless-stopped \
    email-verifier
