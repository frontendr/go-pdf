# syntax=docker/dockerfile:1

# Build stage
FROM --platform=linux/amd64 golang:1.24 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o go-pdf main.go

# Final stage
FROM --platform=linux/amd64 ubuntu:22.04

# Avoid prompts from apt
ENV DEBIAN_FRONTEND=noninteractive

WORKDIR /app

# Install Chrome and dependencies
RUN apt-get update && apt-get install -y \
    wget \
    gnupg \
    ca-certificates \
    fonts-liberation \
    libasound2 \
    libatk-bridge2.0-0 \
    libatk1.0-0 \
    libatspi2.0-0 \
    libcairo2 \
    libcups2 \
    libdbus-1-3 \
    libdrm2 \
    libgbm1 \
    libgtk-3-0 \
    libnspr4 \
    libnss3 \
    libxcomposite1 \
    libxdamage1 \
    libxfixes3 \
    libxkbcommon0 \
    libxrandr2 \
    xdg-utils \
    && wget -q https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb \
    && apt-get install -y ./google-chrome-stable_current_amd64.deb \
    && rm google-chrome-stable_current_amd64.deb \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Copy the binary from builder
COPY --from=builder /app/go-pdf .

# Copy templates directory
COPY --from=builder /app/templates ./templates

# Copy .env.docker as .env for default configuration
COPY .env.docker .env

# Expose port 80
EXPOSE 80

# Run the application
CMD ["./go-pdf"]