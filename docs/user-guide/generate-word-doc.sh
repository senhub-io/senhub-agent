#!/bin/bash
# Generate consolidated Word documentation from Markdown files
# Transforms cross-file links into internal section links
# Converts Mermaid diagrams to PNG images
# Usage: ./generate-word-doc.sh

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
OUTPUT_FILE="$SCRIPT_DIR/SenHub-Agent-User-Guide-Complete.docx"
TEMP_DIR="$SCRIPT_DIR/.temp-word-gen"
MERMAID_DIR="$TEMP_DIR/mermaid-images"

# Clean and create temp directories
rm -rf "$TEMP_DIR"
mkdir -p "$TEMP_DIR"
mkdir -p "$MERMAID_DIR"

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

# Function to extract and convert Mermaid diagrams
process_mermaid() {
    local input_file=$1
    local output_file=$2
    local file_basename=$(basename "$input_file" .md)

    # Python script to extract and replace Mermaid diagrams
    python3 - "$input_file" "$output_file" "$MERMAID_DIR" "$file_basename" << 'PYTHON_SCRIPT'
import sys
import re
import os
import subprocess

input_file = sys.argv[1]
output_file = sys.argv[2]
mermaid_dir = sys.argv[3]
file_basename = sys.argv[4]

with open(input_file, 'r') as f:
    content = f.read()

# Find all mermaid blocks
mermaid_pattern = r'```mermaid\n(.*?)\n```'
matches = list(re.finditer(mermaid_pattern, content, re.DOTALL))

diagram_count = 0
for match in matches:
    diagram_count += 1
    mermaid_code = match.group(1)

    # Create unique filename
    mermaid_file = os.path.join(mermaid_dir, f'{file_basename}-diagram-{diagram_count}.mmd')
    png_file = os.path.join(mermaid_dir, f'{file_basename}-diagram-{diagram_count}.png')

    # Write mermaid code to file
    with open(mermaid_file, 'w') as mf:
        mf.write(mermaid_code)

    # Convert to PNG using mermaid-cli
    try:
        subprocess.run(['mmdc', '-i', mermaid_file, '-o', png_file, '-b', 'transparent'],
                      check=True, capture_output=True)

        # Replace mermaid block with image reference
        image_ref = f'![Diagram {diagram_count}]({png_file})'
        content = content.replace(match.group(0), image_ref, 1)

        print(f'  ✓ Converted diagram {diagram_count} in {file_basename}')
    except subprocess.CalledProcessError as e:
        print(f'  ✗ Failed to convert diagram {diagram_count} in {file_basename}: {e}')
        # Keep original mermaid block on error
        pass

# Write output file
with open(output_file, 'w') as f:
    f.write(content)

print(f'  → Processed {diagram_count} diagrams in {file_basename}')
PYTHON_SCRIPT
}

# Process each file
echo "Converting Mermaid diagrams to images..."
for file in INSTALLATION.md OPERATING-MODES.md AGENT-CONFIGURATION.md \
            HTTP-HTTPS-CONFIGURATION.md PROBES-CONFIGURATION.md WEB-INTERFACE.md \
            METRICS-USAGE.md TROUBLESHOOTING.md; do
    if [ -f "$SCRIPT_DIR/$file" ]; then
        # First transform links
        transform_links "$SCRIPT_DIR/$file" "$TEMP_DIR/${file}.tmp"
        # Then process mermaid diagrams
        process_mermaid "$TEMP_DIR/${file}.tmp" "$TEMP_DIR/$file"
        rm "$TEMP_DIR/${file}.tmp"
    fi
done

echo "Generating consolidated Word documentation..."

pandoc \
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
  --number-sections \
  --metadata title="SenHub Agent - User Guide" \
  --metadata subtitle="Complete Documentation" \
  --metadata author="SenHub" \
  --metadata date="$(date +%Y-%m-%d)"

# Cleanup
rm -rf "$TEMP_DIR"

echo "Documentation generated: $OUTPUT_FILE"
ls -lh "$OUTPUT_FILE"
