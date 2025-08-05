# Stage 1: Build the application
FROM golang:1.24.4-alpine AS builder

# Set the working directory
WORKDIR /app

# Copy and download dependencies first to leverage layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# --- IMPORTANT ---
# Build the application, targeting the ./server directory where main.go lives.
# This creates a static binary named 'server' inside the /app directory.
RUN CGO_ENABLED=0 go build -mod=readonly -v -o server ./server

# ---
# Stage 2: Create the final, minimal production image
FROM gcr.io/distroless/static-debian12

# Set the working directory for the final image
WORKDIR /app

# --- IMPORTANT ---
# Copy the compiled binary AND the templates directory from the builder stage.
COPY --from=builder /app/server .
COPY --from=builder /app/templates ./templates

# --- BEST PRACTICE ---
# Use the non-root user provided by the distroless image
USER nonroot:nonroot

# Expose the port the application listens on
EXPOSE 8080

# The command to run the application
CMD ["./server"]