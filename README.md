# GoCache

GoCache is a local caching proxy for HTTP and HTTPS traffic. It's designed to speed up webscraping or other development workflows by caching frequently requested assets.

While other solutions exist, they can be large (squid/nginx) or require implementing specific cache support in your code.
GoCache uses standard proxy setup configuration in your environment so it should be transparent to your code or require minimal setup (e.g. define proxy in your development configuration)


## Features

-   **HTTP/HTTPS Caching:** Caches responses from both HTTP and HTTPS servers.
-   **MITM Proxy:** Acts as a man-in-the-middle proxy to cache HTTPS traffic.
-   **Negative TTL:** Short cache duration for error responses (4xx/5xx) to reduce upstream load while allowing quick recovery.
-   **POST Caching:** Opt-in caching for POST request responses.
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
