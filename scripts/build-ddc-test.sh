#!/bin/bash

# Build script for DDC test tool
set -e

echo "🔨 Building SenHub Agent DDC Test Tool..."

# Get the current directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Build directory
BUILD_DIR="$PROJECT_ROOT/build"
mkdir -p "$BUILD_DIR"

# Version info
VERSION=${VERSION:-"1.0.0-beta"}
COMMIT_HASH=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo "Version: $VERSION"
echo "Commit: $COMMIT_HASH"
echo "Build Time: $BUILD_TIME"

# Build flags
LDFLAGS="-s -w -X main.version=$VERSION -X main.commit=$COMMIT_HASH -X main.buildTime=$BUILD_TIME"

# Cross-compile for different platforms
declare -A PLATFORMS=(
    ["windows/amd64"]="senhub-ddc-test-windows-amd64.exe"
    ["linux/amd64"]="senhub-ddc-test-linux-amd64"
    ["darwin/amd64"]="senhub-ddc-test-macos-amd64"
    ["darwin/arm64"]="senhub-ddc-test-macos-arm64"
)

cd "$PROJECT_ROOT"

echo "Building binaries..."
for platform in "${!PLATFORMS[@]}"; do
    IFS='/' read -ra PLATFORM_PARTS <<< "$platform"
    GOOS="${PLATFORM_PARTS[0]}"
    GOARCH="${PLATFORM_PARTS[1]}"
    OUTPUT_NAME="${PLATFORMS[$platform]}"
    OUTPUT_PATH="$BUILD_DIR/$OUTPUT_NAME"
    
    echo "  → $platform -> $OUTPUT_NAME"
    
    GOOS=$GOOS GOARCH=$GOARCH go build \
        -ldflags "$LDFLAGS" \
        -o "$OUTPUT_PATH" \
        ./cmd/test-ddc
    
    if [ $? -eq 0 ]; then
        echo "    ✅ Built successfully"
        # Show size
        if command -v ls >/dev/null 2>&1; then
            SIZE=$(ls -lh "$OUTPUT_PATH" | awk '{print $5}')
            echo "    📦 Size: $SIZE"
        fi
    else
        echo "    ❌ Build failed"
    fi
done

# Create deployment package
echo ""
echo "📦 Creating deployment package..."
PACKAGE_DIR="$BUILD_DIR/senhub-ddc-test-$VERSION"
mkdir -p "$PACKAGE_DIR"

# Copy binaries
cp "$BUILD_DIR"/senhub-ddc-test-* "$PACKAGE_DIR/"

# Create usage documentation
cat > "$PACKAGE_DIR/README.md" << 'EOF'
# SenHub Agent - DDC Test Tool

This tool tests the Citrix Delivery Controller (DDC) API connectivity and site filtering capabilities.

## Usage

### Basic Test
```bash
# Windows
senhub-ddc-test-windows-amd64.exe -url "https://your-ddc.domain.com" -username "DOMAIN\username" -password "yourpassword"

# Linux/macOS
./senhub-ddc-test-linux-amd64 -url "https://your-ddc.domain.com" -username "DOMAIN\\username" -password "yourpassword"
```

### Test with Specific Site
```bash
./senhub-ddc-test-linux-amd64 -url "https://your-ddc.domain.com" -username "DOMAIN\\username" -password "yourpassword" -site "SiteName"
```

### Test with Inventory Service
```bash
./senhub-ddc-test-linux-amd64 -url "https://your-ddc.domain.com" -username "DOMAIN\\username" -password "yourpassword" -inventory
```

### Full Test with Verbose Logging
```bash
./senhub-ddc-test-linux-amd64 -url "https://your-ddc.domain.com" -username "DOMAIN\\username" -password "yourpassword" -inventory -verbose
```

## Parameters

- `-url`: Delivery Controller URL (required) - e.g., `https://ddc.domain.com`
- `-username`: Username for authentication (required) - e.g., `DOMAIN\serviceaccount`
- `-password`: Password for authentication (required)
- `-site`: Specific site name to test (optional, uses first site if not specified)
- `-auth`: Authentication method - `basic` or `ntlm` (default: basic)
- `-skip-ssl`: Skip SSL certificate verification (default: true for testing)
- `-inventory`: Test the inventory service functionality
- `-verbose`: Enable verbose logging

## Expected Output

The tool will perform these tests:
1. **Connectivity Test**: Verify DDC API authentication
2. **Sites Discovery**: List all available sites
3. **Machines Test**: Get machines for the specified site
4. **Delivery Groups Test**: Get delivery groups for the site
5. **Site Details**: Get comprehensive site information
6. **Inventory Test** (optional): Test the inventory caching service

## Troubleshooting

### Authentication Issues
- Ensure the username format is correct: `DOMAIN\username`
- Verify the service account has DDC read permissions
- Check if the DDC requires specific authentication method (basic vs ntlm)

### SSL Issues
- Use `-skip-ssl=true` for testing with self-signed certificates
- For production, use valid certificates and `-skip-ssl=false`

### Network Issues
- Verify the DDC URL is accessible from the test machine
- Check firewall rules for HTTPS (443) or custom ports
- Test basic connectivity: `curl -k https://your-ddc.domain.com/cvad/manage/Sites`

### No Sites Found
- Verify the service account has appropriate permissions
- Check if the DDC is properly configured
- Ensure the site exists in the Citrix environment

## Exit Codes
- 0: Success
- 1: Error (check output for details)
EOF

# Create example batch file for Windows
cat > "$PACKAGE_DIR/test-ddc.bat" << 'EOF'
@echo off
echo SenHub DDC Test Tool - Windows Example
echo.

REM Configure these variables
set DDC_URL=https://your-ddc.domain.com
set USERNAME=DOMAIN\serviceaccount
set PASSWORD=yourpassword

echo Testing DDC: %DDC_URL%
echo Username: %USERNAME%
echo.

senhub-ddc-test-windows-amd64.exe -url "%DDC_URL%" -username "%USERNAME%" -password "%PASSWORD%" -inventory -verbose

pause
EOF

# Create example shell script for Linux/macOS
cat > "$PACKAGE_DIR/test-ddc.sh" << 'EOF'
#!/bin/bash

# SenHub DDC Test Tool - Linux/macOS Example

# Configure these variables
DDC_URL="https://your-ddc.domain.com"
USERNAME="DOMAIN\\serviceaccount"
PASSWORD="yourpassword"

echo "🧪 SenHub DDC Test Tool - Linux/macOS Example"
echo "Testing DDC: $DDC_URL"
echo "Username: $USERNAME"
echo ""

# Detect architecture
if [[ $(uname -m) == "arm64" ]] && [[ $(uname) == "Darwin" ]]; then
    BINARY="./senhub-ddc-test-macos-arm64"
elif [[ $(uname) == "Darwin" ]]; then
    BINARY="./senhub-ddc-test-macos-amd64"
else
    BINARY="./senhub-ddc-test-linux-amd64"
fi

echo "Using binary: $BINARY"
chmod +x "$BINARY"

$BINARY -url "$DDC_URL" -username "$USERNAME" -password "$PASSWORD" -inventory -verbose
EOF

chmod +x "$PACKAGE_DIR/test-ddc.sh"

# Create compressed package
cd "$BUILD_DIR"
if command -v tar >/dev/null 2>&1; then
    tar -czf "senhub-ddc-test-$VERSION.tar.gz" "senhub-ddc-test-$VERSION"
    echo "📦 Created package: build/senhub-ddc-test-$VERSION.tar.gz"
fi

if command -v zip >/dev/null 2>&1; then
    zip -r "senhub-ddc-test-$VERSION.zip" "senhub-ddc-test-$VERSION"
    echo "📦 Created package: build/senhub-ddc-test-$VERSION.zip"
fi

echo ""
echo "🎉 Build completed successfully!"
echo ""
echo "Deployment package created at:"
echo "  $PACKAGE_DIR"
echo ""
echo "Usage example:"
echo "  ./senhub-ddc-test-linux-amd64 -url \"https://your-ddc.domain.com\" -username \"DOMAIN\\\\username\" -password \"password\""