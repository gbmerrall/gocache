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

[logging]
level = "info"
file = ""

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

### `[logging]`

| Key     | Type   | Default | Description                                                                 |
| ------- | ------ | ------- | --------------------------------------------------------------------------- |
| `level` | String | "info"  | The log level. Can be one of `debug`, `info`, `warn`, or `error`.             |
| `file`  | String | ""      | The path to a log file. If empty, logs are written to standard output.        |

### `[persistence]`

| Key                  | Type    | Default | Description                                                                                             |
| -------------------- | ------- | ------- | ------------------------------------------------------------------------------------------------------- |
| `enable`             | Boolean | true    | If `true`, the cache will be saved to and loaded from disk.                                             |
| `cache_file`         | String  | `~/.config/gocache/cache.gob` | The path to the file where the cache is persisted.                                    |
| `auto_save_interval` | String  | "5m"    | How often the cache is automatically saved to disk (e.g., "5m", "1h").                                    |
