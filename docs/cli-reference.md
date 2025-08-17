# CLI Reference

GoCache provides a command-line interface for starting the server and managing the cache.

## Starting the Server

To start the GoCache proxy server, simply run the `gocache` command.

```bash
gocache
```

### Flags

| Flag          | Description                                                              |
| ------------- | ------------------------------------------------------------------------ |
| `--config`    | Path to a custom configuration file.                                     |
| `--daemon`    | Run GoCache as a background daemon.                                      |
| `--log-level` | Override the log level from the config file (`debug`, `info`, `warn`, `error`). |

## Management Commands

Management commands are used to interact with a running GoCache instance via the Control API.

### `gocache status`

Displays statistics about the cache.

**Usage:**

```bash
gocache status
```

### `gocache purge <domain>`

Purges all cached items for a specific domain.

**Usage:**

```bash
gocache purge example.com
```

### `gocache purge-url <url>`

Purges a specific URL from the cache.

**Usage:**

```bash
gocache purge-url "https://example.com/some/page"
```

### `gocache purge-all`

Purges the entire cache. You will be prompted for confirmation.

**Usage:**

```bash
gocache purge-all
```

### `gocache export-ca [filename]`

Exports the GoCache root CA certificate to a file.

**Usage:**

```bash
# Export to gocache-ca.crt
gocache export-ca

# Export to a custom filename
gocache export-ca my-ca.crt
```

### `gocache stop`

Stops a running GoCache daemon.

**Usage:**

```bash
gocache stop
```
