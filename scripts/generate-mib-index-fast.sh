#!/usr/bin/env bash

# Fast MIB index generator - simplified version
# Usage: ./generate-mib-index-fast.sh <mibs_directory> <output_json>

# Note: Do NOT use 'set -e' as grep/head commands may return non-zero
# when they don't find matches, which is expected behavior

# Check bash version
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

# Create output directory
mkdir -p "$(dirname "$OUTPUT_JSON")"

# Temporary file for collecting data
TEMP_DATA=$(mktemp)

echo "📦 Processing MIBs..."

# Start timer
start_time=$(date +%s)

# Process each vendor directory
total_vendors=0
total_mibs=0

# First, process vendor subdirectories
for vendor_dir in "$MIBS_DIR"/*/ ; do
    [ -d "$vendor_dir" ] || continue

    vendor_name=$(basename "$vendor_dir")

    # Skip hidden directories and common non-vendor dirs
    [[ "$vendor_name" == .* ]] && continue
    [[ "$vendor_name" == "lost+found" ]] && continue

    ((total_vendors++))
    echo "📂 Scanning vendor: $vendor_name"

    # Strategy: Find ONE enterprise OID (from any MIB in directory)
    # Then index ALL MIBs in this directory under that OID

    # Count files first
    file_count=$(find "$vendor_dir" -maxdepth 1 -type f 2>/dev/null | wc -l)
    echo "  Found $file_count files"

    # Limit processing per vendor to avoid hanging
    files_processed=0
    max_files_per_vendor=500

    # First pass: Find ONE enterprise OID from ANY file
    vendor_oid=""
    all_mib_files=()

    while IFS= read -r file; do
        [ -f "$file" ] || continue

        # Safety limit per vendor
        ((files_processed++))
        if (( files_processed > max_files_per_vendor )); then
            echo "  ⚠️ Stopping at $max_files_per_vendor files for this vendor"
            break
        fi

        filename=$(basename "$file")

        # Skip obvious non-MIB files
        case "$filename" in
            *.txt|*.md|*.json|README*|LICENSE*|*.pdf|*.html) continue ;;
        esac

        # Collect ALL valid MIB files
        all_mib_files+=("$filename")

        # If we haven't found the OID yet, try to extract it from this file
        if [ -z "$vendor_oid" ]; then
            vendor_oid=$(grep -m 1 -oE "(enterprises[[:space:]]+[0-9]+|1[[:space:]]+3[[:space:]]+6[[:space:]]+1[[:space:]]+4[[:space:]]+1[[:space:]]+[0-9]+)" "$file" 2>/dev/null | \
                       sed -E 's/enterprises[[:space:]]+([0-9]+)/1.3.6.1.4.1.\1/; s/1[[:space:]]+3[[:space:]]+6[[:space:]]+1[[:space:]]+4[[:space:]]+1[[:space:]]+([0-9]+)/1.3.6.1.4.1.\1/' || true)

            if [ -n "$vendor_oid" ]; then
                echo "  ✓ Found enterprise OID $vendor_oid in $filename"
            fi
        fi

        ((total_mibs++))

        # Progress indicator
        if (( files_processed % 50 == 0 )); then
            echo "  ... processed $files_processed files"
        fi
    done < <(find "$vendor_dir" -maxdepth 1 -type f 2>/dev/null || true)

    # Build MIB list for this vendor
    mib_list=""
    for mib in "${all_mib_files[@]}"; do
        if [ -n "$mib_list" ]; then
            mib_list+=","
        fi
        mib_list+="{\"name\":\"$mib\",\"file\":\"$mib\"}"
    done

    # Write vendor entry if we found an OID and have MIBs
    if [ -n "$vendor_oid" ] && [ ${#all_mib_files[@]} -gt 0 ]; then
        vendor_display="${vendor_name}"

        # Write vendor entry to temp file
        echo "$vendor_oid|$vendor_display|$vendor_name|$mib_list" >> "$TEMP_DATA"

        echo "  ✓ Indexed: $vendor_oid with ${#all_mib_files[@]} MIBs"
    else
        echo "  ⚠️ No enterprise OID found or no MIBs in directory"
    fi
done

echo ""
echo "📊 Statistics:"
echo "  Vendors scanned: $total_vendors"
echo "  MIB files found: $total_mibs"

# Generate JSON
echo ""
echo "🔨 Building JSON index..."

# Start JSON
cat > "$OUTPUT_JSON" <<EOF
{
  "version": "1.0",
  "generated": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "vendors": {
EOF

# Add vendor entries
first=true
while IFS='|' read -r oid vendor_display vendor_name mib_list; do
    if [ "$first" = true ]; then
        first=false
    else
        echo "," >> "$OUTPUT_JSON"
    fi

    cat >> "$OUTPUT_JSON" <<EOF
    "$oid": {
      "name": "$vendor_display",
      "base_path": "$vendor_name",
      "mibs": [$mib_list]
    }
EOF
done < "$TEMP_DATA"

# Close JSON
cat >> "$OUTPUT_JSON" <<EOF

  }
}
EOF

# Cleanup
rm -f "$TEMP_DATA"

# Count unique vendors
vendor_count=$(grep -c '"name":' "$OUTPUT_JSON" || echo "0")

# Calculate execution time
end_time=$(date +%s)
execution_time=$((end_time - start_time))
minutes=$((execution_time / 60))
seconds=$((execution_time % 60))

echo ""
echo "✅ MIB index generated successfully!"
echo "📄 Output: $OUTPUT_JSON"
echo "📊 Vendors indexed: $vendor_count"

if [ $minutes -gt 0 ]; then
    echo "⏱️  Execution time: ${minutes}m ${seconds}s"
else
    echo "⏱️  Execution time: ${seconds}s"
fi

# Show sample if jq is available
if command -v jq &> /dev/null; then
    echo ""
    echo "📋 Sample entries:"
    jq -r '.vendors | to_entries | .[:5] | .[] | "  \(.key) -> \(.value.name) (\(.value.mibs | length) MIBs)"' "$OUTPUT_JSON" 2>/dev/null || true
fi

exit 0
