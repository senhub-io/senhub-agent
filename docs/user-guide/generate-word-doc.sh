#!/bin/bash
# Generate consolidated Word documentation from Markdown files
# Transforms cross-file links into internal section links
# Usage: ./generate-word-doc.sh

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
OUTPUT_FILE="$SCRIPT_DIR/SenHub-Agent-User-Guide-Complete.docx"
TEMP_DIR="$SCRIPT_DIR/.temp-word-gen"

# Clean and create temp directory
rm -rf "$TEMP_DIR"
mkdir -p "$TEMP_DIR"

echo "Preparing documentation files for Word generation..."

# Function to transform cross-file links to internal links
transform_links() {
    local file=$1
    local temp_file=$2

    sed -E \
        -e 's|\[Installation\]\(\.\/INSTALLATION\.md\)|[Installation](#installation)|g' \
        -e 's|\[Operating Modes\]\(\.\/OPERATING-MODES\.md\)|[Operating Modes](#operating-modes)|g' \
        -e 's|\[Agent Configuration\]\(\.\/AGENT-CONFIGURATION\.md\)|[Agent Configuration](#agent-configuration)|g' \
        -e 's|\[HTTP/HTTPS Configuration\]\(\.\/HTTP-HTTPS-CONFIGURATION\.md\)|[HTTP/HTTPS Configuration](#httphttps-configuration)|g' \
        -e 's|\[HTTP/HTTPS\]\(\.\/HTTP-HTTPS-CONFIGURATION\.md\)|[HTTP/HTTPS Configuration](#httphttps-configuration)|g' \
        -e 's|\[Probes Configuration\]\(\.\/PROBES-CONFIGURATION\.md\)|[Probes Configuration](#probes-configuration)|g' \
        -e 's|\[Probes\]\(\.\/PROBES-CONFIGURATION\.md\)|[Probes Configuration](#probes-configuration)|g' \
        -e 's|\[Web Interface\]\(\.\/WEB-INTERFACE\.md\)|[Web Interface](#web-interface)|g' \
        -e 's|\[Metrics Usage\]\(\.\/METRICS-USAGE\.md\)|[Metrics Usage](#metrics-usage)|g' \
        -e 's|\[Troubleshooting\]\(\.\/TROUBLESHOOTING\.md\)|[Troubleshooting](#troubleshooting)|g' \
        -e 's|\[AGENT-CONFIGURATION\.md\]\(\.\/AGENT-CONFIGURATION\.md#license-system\)|[Agent Configuration - License System](#license-system)|g' \
        -e 's|\[Agent Configuration - License System\]\(\.\/AGENT-CONFIGURATION\.md#license-system\)|[License System](#license-system)|g' \
        -e 's|\[Metrics Usage - Troubleshooting Integration Issues\]\(\.\/METRICS-USAGE\.md#troubleshooting-integration-issues\)|[Troubleshooting Integration Issues](#troubleshooting-integration-issues)|g' \
        -e 's|\[Configure Operating Mode\]\(\.\/OPERATING-MODES\.md\)|[Operating Modes](#operating-modes)|g' \
        -e 's|\[Configure the Agent\]\(\.\/AGENT-CONFIGURATION\.md\)|[Agent Configuration](#agent-configuration)|g' \
        -e 's|\[Add Monitoring Probes\]\(\.\/PROBES-CONFIGURATION\.md\)|[Probes Configuration](#probes-configuration)|g' \
        -e 's|\[Access Web Interface\]\(\.\/WEB-INTERFACE\.md\)|[Web Interface](#web-interface)|g' \
        -e 's|\[Integrate with Tools\]\(\.\/METRICS-USAGE\.md\)|[Metrics Usage](#metrics-usage)|g' \
        "$file" > "$temp_file"
}

# Transform each file
for file in README.md INSTALLATION.md OPERATING-MODES.md AGENT-CONFIGURATION.md \
            HTTP-HTTPS-CONFIGURATION.md PROBES-CONFIGURATION.md WEB-INTERFACE.md \
            METRICS-USAGE.md TROUBLESHOOTING.md; do
    if [ -f "$SCRIPT_DIR/$file" ]; then
        transform_links "$SCRIPT_DIR/$file" "$TEMP_DIR/$file"
    fi
done

echo "Generating consolidated Word documentation..."

pandoc \
  "$TEMP_DIR/README.md" \
  "$TEMP_DIR/INSTALLATION.md" \
  "$TEMP_DIR/OPERATING-MODES.md" \
  "$TEMP_DIR/AGENT-CONFIGURATION.md" \
  "$TEMP_DIR/HTTP-HTTPS-CONFIGURATION.md" \
  "$TEMP_DIR/PROBES-CONFIGURATION.md" \
  "$TEMP_DIR/WEB-INTERFACE.md" \
  "$TEMP_DIR/METRICS-USAGE.md" \
  "$TEMP_DIR/TROUBLESHOOTING.md" \
  -o "$OUTPUT_FILE" \
  --toc \
  --toc-depth=3 \
  --metadata title="SenHub Agent - User Guide Complete" \
  --metadata author="SenHub" \
  --metadata date="$(date +%Y-%m-%d)"

# Cleanup
rm -rf "$TEMP_DIR"

echo "Documentation generated: $OUTPUT_FILE"
ls -lh "$OUTPUT_FILE"
