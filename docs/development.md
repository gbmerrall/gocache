# Development Guide

This guide provides instructions for setting up a development environment for GoCache.

## Prerequisites

- Go 1.18 or later
- Git

## Building from Source

1.  **Clone the repository:**

    ```bash
    git clone https://github.com/gbmerrall/gocache.git
    cd gocache
    ```

2.  **Install dependencies:**

    ```bash
    go mod tidy
    ```

3.  **Build the binary:**

    ```bash
    go build ./cmd/gocache
    ```

## Running Tests

To run the test suite:

```bash
go test ./...
```

## Project Structure

-   `cmd/gocache/`: Main application entry point.
-   `internal/`: Internal packages for GoCache's core logic.
    -   `cache/`: Caching logic.
    -   `cert/`: Certificate generation and management.
    -   `cli/`: CLI command handling.
    -   `config/`: Configuration loading and management.
    -   `control/`: Control API server.
    -   `pidfile/`: PID file management for the daemon.
    -   `proxy/`: The core proxy server.
-   `docs/`: Documentation files.
