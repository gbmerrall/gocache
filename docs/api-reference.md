# Control API Reference

GoCache provides a Control API for managing the cache and the server itself. The Control API listens on the `control_port` specified in the configuration, and for security, it only binds to localhost.

## Endpoints

### `GET /stats`

Returns a JSON object with statistics about the cache.

**Example Response:**

```json
{
    "hit_count": 120,
    "miss_count": 30,
    "hit_rate_percent": "80.00",
    "entry_count": 500,
    "uptime_seconds": "3600.00",
    "cache_size_bytes": 52428800,
    "cert_cache_count": 10
}
```

### `POST /purge/all`

Purges the entire cache.

**Example Response:**

```json
{
    "purged_count": 500
}
```

### `POST /purge/url`

Purges a specific URL from the cache.

**Request Body:**

```json
{
    "url": "https://example.com/some/page"
}
```

**Example Response:**

```json
{
    "url": "https://example.com/some/page",
    "purged": true
}
```

### `POST /purge/domain/:domain`

Purges all cached items for a specific domain.

**Example Request:**

`POST /purge/domain/example.com`

**Example Response:**

```json
{
    "domain": "example.com",
    "purged_count": 50
}
```

### `GET /ca`

Downloads the GoCache root CA certificate in PEM format.

### `GET /health`

Returns health information about the GoCache server.

**Example Response:**

```json
{
    "status": "ok",
    "go_version": "go1.18",
    "uptime": "1h0m0s",
    "config_file": "/home/user/.config/gocache/config.toml"
}
```

### `POST /reload`

Reloads the configuration from the file on disk.

### `POST /shutdown`

Initiates a graceful shutdown of the GoCache server.
