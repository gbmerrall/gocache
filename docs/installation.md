# Installation

This guide provides instructions for installing GoCache on various operating systems.

## Prerequisites

- Go 1.18 or later installed on your system.

## Installation from Source

1.  **Clone the repository:**

    ```bash
    git clone https://github.com/gbmerrall/gocache.git
    cd gocache
    ```

2.  **Build the binary:**

    ```bash
    go build ./cmd/gocache
    ```

3.  **Run GoCache:**

    ```bash
    ./gocache
    ```

## Platform-Specific Instructions

### Linux

Follow the "Installation from Source" instructions above. You can move the compiled `gocache` binary to a directory in your `PATH` for easier access, for example:

```bash
sudo mv gocache /usr/local/bin/
```

### macOS

Follow the "Installation from Source" instructions above. You can also move the compiled `gocache` binary to a directory in your `PATH`:

```bash
sudo mv gocache /usr/local/bin/
```

### Windows

1.  **Clone the repository:**

    ```bash
    git clone https://github.com/gbmerrall/gocache.git
    cd gocache
    ```

2.  **Build the binary:**

    ```bash
    go build -o gocache.exe ./cmd/gocache
    ```

3.  **Run GoCache:**

    ```bash
    .\gocache.exe
    ```

You can add the directory containing `gocache.exe` to your system's `Path` environment variable to run it from any command prompt.
