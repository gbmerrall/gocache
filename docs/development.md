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

### Test Structure

GoCache uses a comprehensive test suite with both unit and integration tests:

- **Unit tests**: Test individual components in isolation
- **Integration tests**: Test the complete proxy functionality using a dedicated test server

### Test Server Infrastructure

The proxy tests use a custom test server (`internal/proxy/testserver.go`) that provides controlled, deterministic endpoints for testing various scenarios:

#### Available Test Endpoints

- **Cacheable Content**: `/cacheable`, `/cacheable-json`, `/cacheable-css`, `/cacheable-js`
- **Non-cacheable Content**: `/non-cacheable`, `/binary`, `/image`
- **Error Responses**: `/error/400`, `/error/404`, `/error/500`, `/error/503`
- **Redirects**: `/redirect/301`, `/redirect/302`, `/redirect/target`
- **Cache Control**: `/no-cache`, `/max-age`, `/expires`
- **Dynamic Content**: `/dynamic`, `/timestamp` (changes on each request)
- **Performance Testing**: `/slow?delay=1000`, `/large?size=1048576`
- **Header Testing**: `/headers` (echoes request headers)

#### Key Test Scenarios

The integration tests cover:

1. **Caching Behavior**: Verifies cacheable vs non-cacheable content types
2. **TTL Expiration**: Tests cache refresh when TTL expires
3. **Negative TTL**: Tests shorter cache duration for error responses (4xx/5xx)
4. **HTTP Methods**: Verifies all HTTP verbs are passed through correctly
5. **Redirects**: Tests 301/302 redirect handling
6. **Large Responses**: Tests caching of multi-megabyte responses
7. **Header Preservation**: Ensures request headers are forwarded properly
8. **Cache Control**: Tests no-cache, max-age, and expires directives

#### Running Specific Tests

```bash
# Run all proxy tests
go test ./internal/proxy

# Run specific test categories
go test ./internal/proxy -run TestCachingBehavior
go test ./internal/proxy -run TestTTLExpiration
go test ./internal/proxy -run TestHTTPVerb

# Run tests with verbose output
go test -v ./internal/proxy
```

#### TTL Expiration Testing

The test suite includes comprehensive TTL expiration testing that:

1. Makes an initial request (cache MISS)
2. Makes a second request within TTL (cache HIT with identical content)
3. Waits for TTL to expire
4. Makes a third request (cache MISS with refreshed content)
5. Makes a fourth request (cache HIT with newly cached content)

This verifies that objects are properly refreshed when their TTL expires.

### Test Server Benefits

The custom test server approach provides:

- **Deterministic testing**: Complete control over responses and timing
- **No external dependencies**: Tests run offline without internet access
- **Performance**: Fast local execution
- **Comprehensive coverage**: All major proxy scenarios covered
- **Easy debugging**: Built-in request counting and logging

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
