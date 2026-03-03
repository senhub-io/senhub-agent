#!/bin/bash
# Generate Word documents for SenHub Agent documentation
# Usage:
#   ./generate-all-modular-docs.sh              # Generate all documents
#   ./generate-all-modular-docs.sh general       # Generate General Guide only
#   ./generate-all-modular-docs.sh citrix        # Generate Citrix Guide only
#   ./generate-all-modular-docs.sh netscaler     # Generate NetScaler Guide only
#   ./generate-all-modular-docs.sh prerequisites # Generate Prerequisites only
#   ./generate-all-modular-docs.sh complete      # Generate all-in-one document

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
REPO_ROOT="$SCRIPT_DIR/../../.."

# Extract version from git tags (strip -beta suffix)
AGENT_VERSION=$(cd "$REPO_ROOT" && git tag -l | grep -E '^[0-9]+\.[0-9]+\.[0-9]+' | sort -V | tail -n1 | sed 's/-beta$//' 2>/dev/null || echo "0.1.80")

TARGET="${1:-all}"

echo "========================================="
echo "SenHub Agent - Documentation Generator"
echo "Version: $AGENT_VERSION"
echo "========================================="
echo ""

# Check dependencies
check_dependencies() {
    local ok=true
    if ! command -v pandoc &> /dev/null; then
        echo "  ERROR: pandoc not found (brew install pandoc)"
        ok=false
    fi
    if ! command -v python3 &> /dev/null; then
        echo "  ERROR: python3 not found"
        ok=false
    fi
    $ok || exit 1

    if ! python3 -c "import docx" 2>/dev/null; then
        echo "  WARNING: python-docx not installed (pip3 install python-docx)"
        echo "  -> Continuing without post-processing..."
        POST_PROCESS=false
    else
        POST_PROCESS=true
    fi
}

check_dependencies

# Temp directory
TEMP_BASE_DIR="$SCRIPT_DIR/.temp-generation"
rm -rf "$TEMP_BASE_DIR"
mkdir -p "$TEMP_BASE_DIR"

# --- Markdown preprocessing functions ---

remove_local_toc() {
    python3 - "$1" "$2" << 'PYEOF'
import sys, re
with open(sys.argv[1]) as f: content = f.read()
content = re.sub(r'## Table of Contents\n.*?\n---\n', '', content, flags=re.DOTALL)
with open(sys.argv[2], 'w') as f: f.write(content)
PYEOF
}

fix_lists() {
    python3 - "$1" "$2" << 'PYEOF'
import sys, re
with open(sys.argv[1]) as f: content = f.read()
content = re.sub(r':(\n)(-\s)', r':\n\n\2', content)
content = re.sub(r':(\n)(\d+\.\s)', r':\n\n\2', content)
content = re.sub(r'(\*\*[^*]+:\*\*)(\n)(-\s)', r'\1\n\n\3', content)
content = re.sub(r'(\*\*[^*]+:\*\*)(\n)(\d+\.\s)', r'\1\n\n\3', content)
with open(sys.argv[2], 'w') as f: f.write(content)
PYEOF
}

transform_links() {
    sed -E \
        -e 's|\[Installation\]\(\.\/INSTALLATION\.md\)|[Installation](#installation)|g' \
        -e 's|\[Agent Configuration\]\(\.\/AGENT-CONFIGURATION\.md\)|[Agent Configuration](#agent-configuration)|g' \
        -e 's|\[HTTP/HTTPS Configuration\]\(\.\/HTTP-HTTPS-CONFIGURATION\.md\)|[HTTP/HTTPS Configuration](#httphttps-configuration)|g' \
        -e 's|\[HTTP/HTTPS\]\(\.\/HTTP-HTTPS-CONFIGURATION\.md\)|[HTTP/HTTPS Configuration](#httphttps-configuration)|g' \
        -e 's|\[Web Interface\]\(\.\/WEB-INTERFACE\.md\)|[Web Interface](#web-interface)|g' \
        -e 's|\[Troubleshooting\]\(\.\/TROUBLESHOOTING\.md\)|[Troubleshooting](#troubleshooting)|g' \
        "$1" > "$2"
}

remove_horizontal_rules() {
    sed '/^---$/d' "$1" > "$2"
}

remove_mermaid_blocks() {
    python3 - "$1" "$2" << 'PYEOF'
import sys, re
with open(sys.argv[1]) as f: content = f.read()
content = re.sub(r'```mermaid\n.*?\n```\n?', '', content, flags=re.DOTALL)
with open(sys.argv[2], 'w') as f: f.write(content)
PYEOF
}

# --- Document generation ---

generate_document() {
    local doc_name=$1
    local title=$2
    local subtitle=$3
    shift 3
    local source_files=("$@")

    echo "-----------------------------------------"
    echo "Generating: $doc_name"
    echo "-----------------------------------------"

    local output_file="$SCRIPT_DIR/SenHub-Agent-${doc_name}.docx"
    local temp_dir="$TEMP_BASE_DIR/$doc_name"
    mkdir -p "$temp_dir"

    # Preprocess each source file
    local processed_files=()
    for source_file in "${source_files[@]}"; do
        local basename=$(basename "$source_file")
        echo "  Processing $basename..."

        remove_local_toc "$source_file" "$temp_dir/${basename}.s1"
        fix_lists "$temp_dir/${basename}.s1" "$temp_dir/${basename}.s2"
        transform_links "$temp_dir/${basename}.s2" "$temp_dir/${basename}.s3"
        remove_horizontal_rules "$temp_dir/${basename}.s3" "$temp_dir/${basename}.s4"
        remove_mermaid_blocks "$temp_dir/${basename}.s4" "$temp_dir/$basename"
        rm -f "$temp_dir/${basename}".s{1,2,3,4}

        processed_files+=("$temp_dir/$basename")
    done

    # Pandoc: Markdown -> Word
    echo "  Pandoc conversion..."
    pandoc \
        "${processed_files[@]}" \
        -o "$output_file" \
        --toc \
        --toc-depth=3 \
        --number-sections \
        --metadata title="$title" \
        --metadata subtitle="$subtitle" \
        --metadata author="SenHub" \
        --metadata date="$(date +%Y-%m-%d)"

    # Post-process: tables, code, visual styling
    if $POST_PROCESS; then
        echo "  Post-processing (tables, code, styling)..."
        python3 "$SCRIPT_DIR/fix-word-formatting.py" "$output_file"
        python3 "$SCRIPT_DIR/apply-visual-style.py" "$output_file"
    fi

    echo "  -> $(ls -lh "$output_file" | awk '{print $5}') $output_file"
    echo ""
}

# --- Document definitions ---

generate_general() {
    generate_document \
        "General-Guide" \
        "SenHub Agent - General Guide" \
        "Installation, Configuration & Operations - v${AGENT_VERSION}" \
        "$SCRIPT_DIR/INSTALLATION-SIMPLE.md" \
        "$SCRIPT_DIR/CONFIGURATION-SIMPLE.md" \
        "$SCRIPT_DIR/HTTP-SIMPLE.md" \
        "$SCRIPT_DIR/WEB-INTERFACE-SIMPLE.md" \
        "$SCRIPT_DIR/TROUBLESHOOTING-SIMPLE.md"
}

generate_citrix() {
    generate_document \
        "Citrix-Guide" \
        "SenHub Agent - Citrix Guide" \
        "Citrix Virtual Apps and Desktops Monitoring - v${AGENT_VERSION}" \
        "$SCRIPT_DIR/CITRIX-GUIDE.md"
}

generate_netscaler() {
    generate_document \
        "NetScaler-Guide" \
        "SenHub Agent - NetScaler Guide" \
        "Citrix ADC Load Balancer Monitoring - v${AGENT_VERSION}" \
        "$SCRIPT_DIR/NETSCALER-GUIDE.md"
}

generate_prerequisites() {
    generate_document \
        "Prerequisites-Citrix-NetScaler" \
        "SenHub Agent - Prerequis Citrix et NetScaler" \
        "Supervision Citrix CVAD et NetScaler ADC - v${AGENT_VERSION}" \
        "$SCRIPT_DIR/PREREQUISITES-CITRIX-NETSCALER.md"
}

generate_complete() {
    generate_document \
        "User-Guide-Complete" \
        "SenHub Agent - User Guide" \
        "Complete Documentation - v${AGENT_VERSION}" \
        "$SCRIPT_DIR/INSTALLATION-SIMPLE.md" \
        "$SCRIPT_DIR/CONFIGURATION-SIMPLE.md" \
        "$SCRIPT_DIR/HTTP-SIMPLE.md" \
        "$SCRIPT_DIR/WEB-INTERFACE-SIMPLE.md" \
        "$SCRIPT_DIR/TROUBLESHOOTING-SIMPLE.md" \
        "$SCRIPT_DIR/CITRIX-GUIDE.md" \
        "$SCRIPT_DIR/NETSCALER-GUIDE.md" \
        "$SCRIPT_DIR/PREREQUISITES-CITRIX-NETSCALER.md"
}

# --- Main ---

case "$TARGET" in
    general)       generate_general ;;
    citrix)        generate_citrix ;;
    netscaler)     generate_netscaler ;;
    prerequisites) generate_prerequisites ;;
    complete)      generate_complete ;;
    all)
        generate_general
        generate_citrix
        generate_netscaler
        generate_prerequisites
        ;;
    *)
        echo "Unknown target: $TARGET"
        echo ""
        echo "Usage: $0 [general|citrix|netscaler|prerequisites|complete|all]"
        exit 1
        ;;
esac

# Cleanup
rm -rf "$TEMP_BASE_DIR"

echo "========================================="
echo "Done!"
echo "========================================="
ls -lh "$SCRIPT_DIR"/SenHub-Agent-*.docx 2>/dev/null | awk '{print "  " $5 "\t" $9}'
echo ""
