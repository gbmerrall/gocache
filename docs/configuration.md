# Configuration

GoCache is configured using a TOML file. By default, it looks for a file named `gocache.toml` in the current directory.

## Configuration File Location

GoCache searches for a configuration file in the following locations, in order:

1.  The path specified by the `--config` command-line flag.
2.  `./gocache.toml`
3.  `$HOME/.config/gocache/config.toml`
4.  `$HOME/.gocache.toml`
5.  `/etc/gocache/config.toml`

If no configuration file is found, GoCache will use default values.

## Configuration Options

Here is an example `gocache.toml` file with all available options:

```toml
[server]
proxy_port = 8080
control_port = 8081
bind_address = "127.0.0.1"

[cache]
default_ttl = "1h"
negative_ttl = "10s"
max_size_mb = 500
ignore_no_cache = false
cacheable_types = [
    "text/html",
    "text/css",
    "application/javascript",
    "application/json",
    "text/plain"
]

[cache.post_cache]
enable = false
include_query_string = false
max_request_body_size_mb = 10
max_response_body_size_mb = 10

[logging]
# Application logs (for developers/debugging)
# Application logging is disabled by default (set to empty string)
level = ""        # Legacy setting, use app_level instead
file = ""         # Legacy setting, use app_logfile instead

# New application logging configuration
app_level = ""    # Application log level: debug, info, warn, error (empty = disabled)
app_logfile = ""  # Application log file path (empty = stdout only)

# Access logs (for operators/monitoring)
access_to_stdout = true    # Write access logs to stdout (auto-detected: true for foreground, false for daemon)
access_logfile = ""        # Access log file path (empty = no file logging)
access_format = "human"    # Access log format: "human" or "json"

[persistence]
enable = true
cache_file = "" # Default: ~/.config/gocache/cache.gob
auto_save_interval = "5m"
```

### `[server]`

| Key            | Type   | Default     | Description                                                                                             |
| -------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------- |
| `proxy_port`   | Integer| 8080        | The port for the main caching proxy server.                                                             |
| `control_port` | Integer| 8081        | The port for the Control API server.                                                                    |
| `bind_address` | String | "127.0.0.1" | The IP address to bind both servers to. **For security, the Control API only binds to localhost.**      |

### `[cache]`

| Key               | Type           | Default                                                              | Description                                                                                                                               |
| ----------------- | -------------- | -------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `default_ttl`     | String         | "1h"                                                                 | The default time-to-live for cached items (e.g., "30m", "1h", "24h").                                                                     |
| `negative_ttl`    | String         | "10s"                                                                | The time-to-live for error responses (4xx/5xx status codes). Should be shorter than default_ttl to allow quick recovery from temporary errors. |
| `max_size_mb`     | Integer        | 500                                                                  | The maximum size of the cache in megabytes.                                                                                               |
| `ignore_no_cache` | Boolean        | false                                                                | If `true`, GoCache will cache responses even if they have `Cache-Control: no-cache` or `Pragma: no-cache` headers.                        |
| `cacheable_types` | Array of Strings | `["text/html", "text/css", "application/javascript", "application/json", "text/plain"]` | A list of `Content-Type` values that are eligible for caching.                                                                    |

### `[cache.post_cache]`

This section controls the optional caching of `POST` request responses. By default, this is disabled. When enabled, the cache key is generated from a SHA256 hash of the request body.

| Key                         | Type    | Default | Description                                                                                                                               |
| --------------------------- | ------- | ------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `enable`                    | Boolean | false   | If `true`, enables caching for `POST` requests.                                                                                           |
| `include_query_string`      | Boolean | false   | If `true`, the URL's query string will be included in the cache key calculation.                                                          |
| `max_request_body_size_mb`  | Integer | 10      | The maximum size in megabytes for a `POST` request body to be eligible for caching. Requests with larger bodies will not be cached.        |
| `max_response_body_size_mb` | Integer | 10      | The maximum size in megabytes for a `POST` response body to be eligible for caching. Responses with larger bodies will not be cached.      |

**Note:** There is a hard-coded maximum limit of 50MB for `max_request_body_size_mb` and `max_response_body_size_mb`. If you configure a value higher than 50, it will be capped at 50MB and a warning will be logged.

### `[logging]`

GoCache supports two types of logging:
- **Application logs**: For developers and debugging (disabled by default)
- **Access logs**: For operators and monitoring HTTP requests

#### Application Logging (Legacy)

| Key     | Type   | Default | Description                                                                 |
| ------- | ------ | ------- | --------------------------------------------------------------------------- |
| `level` | String | ""      | **Deprecated:** Use `app_level` instead. The log level. Can be one of `debug`, `info`, `warn`, or `error`. Empty string disables logging. |
| `file`  | String | ""      | **Deprecated:** Use `app_logfile` instead. The path to a log file. If empty, logs are written to standard output. |

#### Application Logging (New)

| Key          | Type   | Default | Description                                                                 |
| ------------ | ------ | ------- | --------------------------------------------------------------------------- |
| `app_level`  | String | ""      | Application log level. Can be one of `debug`, `info`, `warn`, or `error`. Empty string disables application logging (default). |
| `app_logfile`| String | ""      | Application log file path. If empty, application logs are written to standard output (when enabled). |

#### Access Logging

Access logs record HTTP request details in a structured format for monitoring and analysis.

| Key                | Type    | Default  | Description                                                                 |
| ------------------ | ------- | -------- | --------------------------------------------------------------------------- |
| `access_to_stdout` | Boolean | true     | Write access logs to standard output. Auto-detected based on process mode: `true` for foreground processes, `false` for daemon processes. |
| `access_logfile`   | String  | ""       | Access log file path. If empty, no access logs are written to file. |
| `access_format`    | String  | "human"  | Access log format. Can be `"human"` (space-separated) or `"json"`. |

#### Access Log Format

Access logs contain 8 fields in the following order:

1. **Timestamp** (ISO8601 with second precision)
2. **Cache Status** (`HIT`, `MISS`, or empty for non-cacheable requests)
3. **HTTP Status Code**
4. **HTTP Method**
5. **Response Size** (bytes)
6. **Response Time** (milliseconds)
7. **Request URL**
8. **Content Type**

**Human format example:**
```
2025-08-19T14:30:45Z HIT 200 GET 1024 15 https://example.com/api/data application/json
2025-08-19T14:30:46Z MISS 404 GET 512 8 https://example.com/missing.html text/html
2025-08-19T14:30:47Z "" 201 POST 256 45 https://example.com/api/submit application/json
```

**JSON format example:**
```json
{"timestamp":"2025-08-19T14:30:45Z","cache_status":"HIT","status":200,"method":"GET","size":1024,"duration_ms":15,"url":"https://example.com/api/data","content_type":"application/json"}
```

#### Notes

- **Application logging is disabled by default** to reduce noise. Enable it by setting `app_level` to a valid level.
- **Legacy settings** (`level`, `file`) are supported for backward compatibility but `app_level` and `app_logfile` take precedence.
- **Access logging** automatically detects foreground vs daemon mode and adjusts stdout output accordingly.
- **Error resilience**: If log files become unavailable, GoCache continues serving requests and logs errors.

### `[persistence]`

| Key                  | Type    | Default | Description                                                                                             |
| -------------------- | ------- | ------- | ------------------------------------------------------------------------------------------------------- |
| `enable`             | Boolean | true    | If `true`, the cache will be saved to and loaded from disk.                                             |
| `cache_file`         | String  | `~/.config/gocache/cache.gob` | The path to the file where the cache is persisted.                                    |
| `auto_save_interval` | String  | "5m"    | How often the cache is automatically saved to disk (e.g., "5m", "1h").                                    |
