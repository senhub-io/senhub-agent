#!/usr/bin/env bash

# Script to generate MIB index JSON from LibreNMS MIB repository
# Usage: ./generate-mib-index.sh <mibs_directory> <output_json>
# Example: ./generate-mib-index.sh /private/tmp/librenms-mibs/mibs ./mibs/index.json

set -e

# Check bash version (need 4+ for associative arrays)
if [ "${BASH_VERSINFO[0]}" -lt 4 ]; then
    echo "❌ Error: This script requires Bash 4.0 or higher"
    echo "Current version: $BASH_VERSION"
    exit 1
fi

MIBS_DIR="${1:-/private/tmp/librenms-mibs/mibs}"
OUTPUT_JSON="${2:-./mibs/index.json}"

if [ ! -d "$MIBS_DIR" ]; then
    echo "Error: MIBs directory not found: $MIBS_DIR"
    exit 1
fi

echo "🔍 Scanning MIBs in: $MIBS_DIR"
echo "📝 Output will be written to: $OUTPUT_JSON"

# Create output directory if it doesn't exist
mkdir -p "$(dirname "$OUTPUT_JSON")"

# Temporary file for processing
TEMP_FILE=$(mktemp)

# Start JSON structure
cat > "$TEMP_FILE" <<'EOF'
{
  "version": "1.0",
  "generated": "",
  "vendors": {}
}
EOF

# Function to extract enterprise OID from a MIB file
extract_enterprise_oid() {
    local file="$1"

    # Quick check: only process first 200 lines for performance
    # Pattern 1: vendor OBJECT IDENTIFIER ::= { enterprises NNN }
    oid=$(head -200 "$file" 2>/dev/null | \
          grep -m 1 -E "OBJECT IDENTIFIER\s*::=\s*\{\s*enterprises\s+[0-9]+" | \
          sed -E 's/.*enterprises[[:space:]]+([0-9]+).*/1.3.6.1.4.1.\1/' || true)

    if [ -z "$oid" ]; then
        # Pattern 2: vendor OBJECT IDENTIFIER ::= { 1 3 6 1 4 1 NNN }
        oid=$(head -200 "$file" 2>/dev/null | \
              grep -m 1 -E "OBJECT IDENTIFIER\s*::=\s*\{\s*1\s+3\s+6\s+1\s+4\s+1\s+[0-9]+" | \
              sed -E 's/.*1[[:space:]]+3[[:space:]]+6[[:space:]]+1[[:space:]]+4[[:space:]]+1[[:space:]]+([0-9]+).*/1.3.6.1.4.1.\1/' || true)
    fi

    echo "$oid"
}

# Function to extract vendor name from MIB
extract_vendor_name() {
    local file="$1"

    # Try to extract from ORGANIZATION field (first 100 lines only)
    vendor=$(head -100 "$file" 2>/dev/null | \
             grep -A 1 "ORGANIZATION" | \
             grep -v "ORGANIZATION" | \
             sed 's/^[[:space:]]*"//;s/"[[:space:]]*$//' | \
             head -1 || true)

    # Fallback: use directory name
    if [ -z "$vendor" ]; then
        vendor=$(dirname "$file" | xargs basename)
    fi

    echo "$vendor"
}

# Associative array to store vendor data
declare -A vendors
declare -A vendor_paths
declare -A vendor_names

echo "📦 Processing MIBs..."

# Scan all MIB files
total_files=0
processed_files=0

# Find all directories with MIBs (vendor folders)
echo "🔍 Finding vendor directories..."
vendor_dirs=$(find "$MIBS_DIR" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort)

if [ -z "$vendor_dirs" ]; then
    echo "⚠️  No vendor directories found, scanning root directory..."
    vendor_dirs="$MIBS_DIR"
fi

# Process each vendor directory
for vendor_dir in $vendor_dirs; do
    vendor_name=$(basename "$vendor_dir")

    # Skip hidden directories
    if [[ "$vendor_name" == .* ]]; then
        continue
    fi

    echo "📂 Scanning vendor: $vendor_name"

    # Find MIB files in this vendor directory
    while IFS= read -r file; do
        ((total_files++))

        # Show progress every 100 files
        if (( total_files % 100 == 0 )); then
            echo "  ... processed $total_files files ($processed_files with OID)"
        fi

        filename=$(basename "$file")
        relative_path=$(dirname "$file" | sed "s|^$MIBS_DIR/||")

        # Extract enterprise OID
        oid=$(extract_enterprise_oid "$file")

        if [ -n "$oid" ]; then
            vendor_name_from_mib=$(extract_vendor_name "$file")

            # Store vendor info
            if [ -z "${vendors[$oid]}" ]; then
                vendors[$oid]="$filename"
                vendor_paths[$oid]="$relative_path"
                vendor_names[$oid]="$vendor_name_from_mib"
            else
                # Append to existing list
                vendors[$oid]="${vendors[$oid]}|$filename"
            fi

            ((processed_files++))
            echo "  ✓ Found: $oid -> $relative_path/$filename"
        fi
    done < <(find "$vendor_dir" -maxdepth 1 -type f ! -name "*.txt" ! -name "*.md" ! -name "*.json" ! -name "README*" 2>/dev/null)
done

echo ""
echo "📊 Statistics:"
echo "  Total files scanned: $total_files"
echo "  MIBs with enterprise OID: $processed_files"
echo "  Unique vendors: ${#vendors[@]}"
echo ""

# Generate JSON content
echo "🔨 Building JSON index..."

# Create JSON structure using jq if available, otherwise use manual approach
if command -v jq &> /dev/null; then
    # Use jq for proper JSON generation
    jq_data='{"version":"1.0","generated":"'$(date -u +"%Y-%m-%dT%H:%M:%SZ")'","vendors":{}}'

    for oid in "${!vendors[@]}"; do
        path="${vendor_paths[$oid]}"
        name="${vendor_names[$oid]}"
        mib_list="${vendors[$oid]}"

        # Build MIBs array
        mibs_json="[]"
        IFS='|' read -ra MIB_FILES <<< "$mib_list"
        for mib in "${MIB_FILES[@]}"; do
            mibs_json=$(echo "$mibs_json" | jq --arg mib "$mib" '. += [{"name": $mib, "file": $mib}]')
        done

        # Add vendor to index
        jq_data=$(echo "$jq_data" | jq \
            --arg oid "$oid" \
            --arg name "$name" \
            --arg path "$path" \
            --argjson mibs "$mibs_json" \
            '.vendors[$oid] = {"name": $name, "base_path": $path, "mibs": $mibs}')
    done

    echo "$jq_data" | jq '.' > "$OUTPUT_JSON"
else
    # Fallback: manual JSON generation (not as clean but works)
    {
        echo "{"
        echo "  \"version\": \"1.0\","
        echo "  \"generated\": \"$(date -u +"%Y-%m-%dT%H:%M:%SZ")\","
        echo "  \"vendors\": {"

        first=true
        for oid in "${!vendors[@]}"; do
            if [ "$first" = false ]; then
                echo ","
            fi
            first=false

            path="${vendor_paths[$oid]}"
            name="${vendor_names[$oid]}"
            mib_list="${vendors[$oid]}"

            echo -n "    \"$oid\": {"
            echo -n "\"name\": \"$name\", "
            echo -n "\"base_path\": \"$path\", "
            echo -n "\"mibs\": ["

            IFS='|' read -ra MIB_FILES <<< "$mib_list"
            first_mib=true
            for mib in "${MIB_FILES[@]}"; do
                if [ "$first_mib" = false ]; then
                    echo -n ", "
                fi
                first_mib=false
                echo -n "{\"name\": \"$mib\", \"file\": \"$mib\"}"
            done

            echo -n "]}"
        done

        echo ""
        echo "  }"
        echo "}"
    } > "$OUTPUT_JSON"
fi

rm -f "$TEMP_FILE"

echo ""
echo "✅ MIB index generated successfully!"
echo "📄 Output: $OUTPUT_JSON"
echo "📊 Vendors indexed: ${#vendors[@]}"

# Show sample of generated index
if command -v jq &> /dev/null; then
    echo ""
    echo "📋 Sample entries:"
    jq -r '.vendors | to_entries | .[:3] | .[] | "  \(.key) -> \(.value.name) (\(.value.mibs | length) MIBs)"' "$OUTPUT_JSON"
fi

exit 0
