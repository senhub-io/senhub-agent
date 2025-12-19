#!/bin/bash
# Generate consolidated Word documentation from Markdown files
# Usage: ./generate-word-doc.sh

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
OUTPUT_FILE="$SCRIPT_DIR/SenHub-Agent-User-Guide-Complete.docx"

echo "Generating consolidated Word documentation..."

pandoc \
  "$SCRIPT_DIR/README.md" \
  "$SCRIPT_DIR/INSTALLATION.md" \
  "$SCRIPT_DIR/OPERATING-MODES.md" \
  "$SCRIPT_DIR/AGENT-CONFIGURATION.md" \
  "$SCRIPT_DIR/HTTP-HTTPS-CONFIGURATION.md" \
  "$SCRIPT_DIR/PROBES-CONFIGURATION.md" \
  "$SCRIPT_DIR/WEB-INTERFACE.md" \
  "$SCRIPT_DIR/METRICS-USAGE.md" \
  "$SCRIPT_DIR/TROUBLESHOOTING.md" \
  -o "$OUTPUT_FILE" \
  --toc \
  --toc-depth=3 \
  --metadata title="SenHub Agent - User Guide Complete" \
  --metadata author="SenHub" \
  --metadata date="$(date +%Y-%m-%d)"

echo "Documentation generated: $OUTPUT_FILE"
ls -lh "$OUTPUT_FILE"
