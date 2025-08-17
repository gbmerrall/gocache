# GoCache Build & Release Guide

This guide covers how to build GoCache for multiple platforms and create releases on GitHub.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Building for Multiple Platforms](#building-for-multiple-platforms)
- [Creating GitHub Releases](#creating-github-releases)
- [Build Scripts](#build-scripts)
- [Testing Builds](#testing-builds)

## Prerequisites

- Go 1.25.0 or later
- Git
- GitHub account with repository access
- (Optional) `make` for using the Makefile

## Building for Multiple Platforms

### Quick Cross-Compilation Commands

Go supports cross-compilation out of the box using environment variables:

```bash
# Linux AMD64
GOOS=linux GOARCH=amd64 go build -o gocache-linux-amd64 ./cmd/gocache

# macOS AMD64
GOOS=darwin GOARCH=amd64 go build -o gocache-darwin-amd64 ./cmd/gocache

# macOS ARM64 (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o gocache-darwin-arm64 ./cmd/gocache

# Windows AMD64
GOOS=windows GOARCH=amd64 go build -o gocache-windows-amd64.exe ./cmd/gocache

# Linux ARM64
GOOS=linux GOARCH=arm64 go build -o gocache-linux-arm64 ./cmd/gocache

# Windows ARM64
GOOS=windows GOARCH=arm64 go build -o gocache-windows-arm64.exe ./cmd/gocache
```

### Build All Platforms at Once

Create a `build.sh` script:

```bash
#!/bin/bash
set -e

VERSION=${1:-$(git describe --tags --always --dirty)}
BUILD_DIR="build"
BINARY_NAME="gocache"

echo "Building GoCache version: $VERSION"
mkdir -p $BUILD_DIR

# Build for all platforms
echo "Building for Linux AMD64..."
GOOS=linux GOARCH=amd64 go build \
    -ldflags "-X main.version=$VERSION" \
    -o $BUILD_DIR/$BINARY_NAME-linux-amd64 \
    ./cmd/gocache

echo "Building for Linux ARM64..."
GOOS=linux GOARCH=arm64 go build \
    -ldflags "-X main.version=$VERSION" \
    -o $BUILD_DIR/$BINARY_NAME-linux-arm64 \
    ./cmd/gocache

echo "Building for macOS AMD64..."
GOOS=darwin GOARCH=amd64 go build \
    -ldflags "-X main.version=$VERSION" \
    -o $BUILD_DIR/$BINARY_NAME-darwin-amd64 \
    ./cmd/gocache

echo "Building for macOS ARM64..."
GOOS=darwin GOARCH=arm64 go build \
    -ldflags "-X main.version=$VERSION" \
    -o $BUILD_DIR/$BINARY_NAME-darwin-arm64 \
    ./cmd/gocache

echo "Building for Windows AMD64..."
GOOS=windows GOARCH=amd64 go build \
    -ldflags "-X main.version=$VERSION" \
    -o $BUILD_DIR/$BINARY_NAME-windows-amd64.exe \
    ./cmd/gocache

echo "Building for Windows ARM64..."
GOOS=windows GOARCH=arm64 go build \
    -ldflags "-X main.version=$VERSION" \
    -o $BUILD_DIR/$BINARY_NAME-windows-arm64.exe \
    ./cmd/gocache

# Generate checksums
echo "Generating checksums..."
cd $BUILD_DIR
sha256sum * > checksums.txt

echo "Build complete! Files in $BUILD_DIR/:"
ls -la
```

Make it executable and run:
```bash
chmod +x build.sh
./build.sh v1.0.0
```

## Creating GitHub Releases

### Step 1: Prepare Your Release

1. **Build all platform binaries** using the build script above
2. **Test your binaries** on different platforms (if possible)
3. **Prepare release notes** with features, changes, and installation instructions

### Step 2: Create and Push Git Tag

```bash
# Create an annotated tag
git tag -a v1.0.0 -m "Release v1.0.0: Initial GoCache release"

# Push the tag to GitHub
git push origin v1.0.0
```

### Step 3: Create Release on GitHub Web Interface

1. **Navigate to your repository** on GitHub
2. **Click "Releases"** in the right sidebar
3. **Click "Create a new release"**
4. **Fill in the release form:**

   - **Tag version**: `v1.0.0` (should auto-populate if you pushed the tag)
   - **Release title**: `GoCache v1.0.0 - Initial Release`
   - **Description**: Use the template below

### Step 4: Release Description Template

```markdown
## GoCache v1.0.0

### What's New
- Initial release of GoCache HTTP/HTTPS caching proxy
- MITM HTTPS interception with dynamic certificate generation
- Control API for cache management
- CLI tools for cache operations

### Features
- HTTP proxy with intelligent caching
- HTTPS MITM interception and caching
- Automatic CA certificate generation
- Cache persistence across restarts
- Graceful shutdown and signal handling
- Configuration reload via SIGHUP

### Installation

Download the appropriate binary for your platform:

| Platform | Architecture | Download |
|----------|-------------|----------|
| Linux | AMD64 | [gocache-linux-amd64](link-will-be-auto-generated) |
| Linux | ARM64 | [gocache-linux-arm64](link-will-be-auto-generated) |
| macOS | AMD64 | [gocache-darwin-amd64](link-will-be-auto-generated) |
| macOS | ARM64 | [gocache-darwin-arm64](link-will-be-auto-generated) |
| Windows | AMD64 | [gocache-windows-amd64.exe](link-will-be-auto-generated) |
| Windows | ARM64 | [gocache-windows-arm64.exe](link-will-be-auto-generated) |

### Quick Start

```bash
# Make the binary executable (Linux/macOS)
chmod +x gocache-*

# Start the proxy
./gocache-linux-amd64

# In another terminal, check status
./gocache-linux-amd64 status

# Export CA certificate for HTTPS
./gocache-linux-amd64 export-ca
```

### Configuration

Create a `gocache.toml` file:

```toml
[server]
proxy_port = 8080
control_port = 8081
bind_address = "127.0.0.1"

[cache]
default_ttl = "1h"
max_size_mb = 500
ignore_no_cache = false

[logging]
level = "info"
file = ""

[persistence]
enable = true
cache_file = ""
auto_save_interval = "5m"
```

### Known Issues
- None at this time

### Full Changelog
- Initial release with complete HTTP/HTTPS caching proxy functionality
```

### Step 5: Upload Binary Assets

1. **Drag and drop** or **click "Attach binaries"**
2. **Upload all platform binaries:**
   - `gocache-linux-amd64`
   - `gocache-linux-arm64`
   - `gocache-darwin-amd64`
   - `gocache-darwin-arm64`
   - `gocache-windows-amd64.exe`
   - `gocache-windows-arm64.exe`
   - `checksums.txt`

### Step 6: Publish Release

- **For final release**: Click "Publish release"
- **For testing**: Click "Save draft" to review later

## Build Scripts

### Option 1: Shell Script (build.sh)

Use the shell script provided above. Run with:
```bash
./build.sh v1.0.0
```

### Option 2: Makefile

Create a `Makefile`:

```makefile
.PHONY: build clean release

VERSION ?= $(shell git describe --tags --always --dirty)
BUILD_DIR = build
BINARY_NAME = gocache

# Build for all platforms
release: clean
	@echo "Building GoCache version: $(VERSION)"
	@mkdir -p $(BUILD_DIR)
	
	GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/gocache
	GOOS=linux GOARCH=arm64 go build -ldflags "-X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/gocache
	GOOS=darwin GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/gocache
	GOOS=darwin GOARCH=arm64 go build -ldflags "-X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/gocache
	GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/gocache
	GOOS=windows GOARCH=arm64 go build -ldflags "-X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-arm64.exe ./cmd/gocache
	
	@echo "Generating checksums..."
	cd $(BUILD_DIR) && sha256sum * > checksums.txt
	@ls -la $(BUILD_DIR)

# Clean build directory
clean:
	rm -rf $(BUILD_DIR)

# Build for current platform only
build:
	go build -o $(BINARY_NAME) ./cmd/gocache
```

Run with:
```bash
make release VERSION=v1.0.0
```

## Testing Builds

### Verify Binary Types

```bash
# Test file types (Linux/macOS)
file build/gocache-linux-amd64     # Should show: ELF 64-bit LSB executable
file build/gocache-darwin-amd64    # Should show: Mach-O 64-bit x86_64 executable
file build/gocache-windows-amd64.exe # Should show: PE32+ executable
```

### Test Binary Execution

```bash
# Test version flag (if implemented)
./build/gocache-linux-amd64 --version

# Test help flag
./build/gocache-linux-amd64 --help
```

### Verify Checksums

```bash
# Verify checksums
cd build
sha256sum -c checksums.txt
```

## Release Checklist

Before creating a release:

- [ ] All tests pass (`go test ./...`)
- [ ] Code builds without warnings
- [ ] Version number is updated
- [ ] Release notes are prepared
- [ ] All platform binaries are built and tested
- [ ] Checksums are generated
- [ ] Git tag is created and pushed
- [ ] GitHub release is created with proper description
- [ ] All binary assets are uploaded
- [ ] Release is published (not draft)

## Versioning

Follow semantic versioning (semver):

- `v1.0.0` - Major release (breaking changes)
- `v1.1.0` - Minor release (new features, backward compatible)
- `v1.0.1` - Patch release (bug fixes, backward compatible)

## Troubleshooting

### Common Build Issues

1. **CGO errors**: GoCache uses only standard library, so CGO should be disabled by default
2. **Missing dependencies**: Run `go mod download` before building
3. **Permission errors**: Ensure you have write permissions in the build directory

### Common Release Issues

1. **Tag not found**: Ensure you've pushed the tag to GitHub (`git push origin v1.0.0`)
2. **Upload fails**: Check file size limits (GitHub has a 2GB limit per file)
3. **Binary won't run**: Ensure correct platform/architecture and executable permissions

## Support

For issues with building or releasing GoCache:

1. Check the [GitHub Issues](https://github.com/username/gocache/issues)
2. Review the build logs for error messages
3. Ensure all prerequisites are installed and up to date
