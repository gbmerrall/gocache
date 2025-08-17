# GoCache

GoCache is a local caching proxy for HTTP and HTTPS traffic. It's designed to speed up development workflows by caching frequently requested assets.

## Features

-   **HTTP/HTTPS Caching:** Caches responses from both HTTP and HTTPS servers.
-   **MITM Proxy:** Acts as a man-in-the-middle proxy to cache HTTPS traffic.
-   **Smart TTL Management:** Different cache lifetimes for successful vs. error responses.
-   **Negative TTL:** Short cache duration for error responses (4xx/5xx) to reduce upstream load while allowing quick recovery.
-   **Configurable:** Easily configured with a TOML file.
-   **Control API:** A simple API for managing the cache.
-   **CLI:** A command-line interface for interacting with the Control API.

## Documentation

-   [Installation](./docs/installation.md)
-   [Configuration](./docs/configuration.md)
-   [CLI Reference](./docs/cli-reference.md)
-   [API Reference](./docs/api-reference.md)
-   [CA Certificate Installation](./docs/ca-installation.md)
-   [Development Guide](./docs/development.md)

## Quick Start

1.  **Install GoCache:** See the [installation guide](./docs/installation.md).
2.  **Run GoCache:**
    ```bash
    gocache
    ```
3.  **Configure your browser or system to use the proxy:**
    -   **Proxy Server:** `127.0.0.1`
    -   **Port:** `8080`
4.  **Install the CA certificate:** See the [CA certificate installation guide](./docs/ca-installation.md).

## License

This project is licensed under the MIT License.
