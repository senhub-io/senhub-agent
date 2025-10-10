#!/usr/bin/env bash

# Generate MIB index from remote repository
# This script queries the remote MIB repo directly without downloading all files

BASE_URL="${1:-https://eu-west-1.intake.senhub.io/mibs}"
OUTPUT_JSON="${2:-./mibs/index.json}"

echo "🌐 Querying remote MIB repository: $BASE_URL"
echo "📝 Output will be written to: $OUTPUT_JSON"

# Create output directory
mkdir -p "$(dirname "$OUTPUT_JSON")"

# Temporary file
TEMP_DATA=$(mktemp)

# List of known vendor directories (from observation)
# Add more as needed
VENDORS=(
    "3com"
    "arista"
    "aruba"
    "cisco"
    "comware"
    "dell"
    "extreme"
    "fortinet"
    "hp"
    "huawei"
    "juniper"
    "paloalto"
    "ubiquiti"
)

# Process each vendor
for vendor in "${VENDORS[@]}"; do
    echo "📂 Processing vendor: $vendor"

    # Fetch directory listing
    listing=$(curl -s "$BASE_URL/$vendor/" || echo "")

    if [ -z "$listing" ]; then
        echo "  ⚠️ Could not fetch listing for $vendor"
        continue
    fi

    # Extract MIB filenames (files without extensions like .txt, .html, etc.)
    mib_files=$(echo "$listing" | grep -o 'href="[^"]*"' | sed 's/href="//;s/"//' | grep -v '/$' | grep -v '\.' | grep -v 'index' | sort -u)

    if [ -z "$mib_files" ]; then
        echo "  ⚠️ No MIBs found for $vendor"
        continue
    fi

    # Count MIBs
    mib_count=$(echo "$mib_files" | wc -l)
    echo "  Found $mib_count MIBs"

    # Download first MIB to extract OID
    first_mib=$(echo "$mib_files" | head -1)
    mib_content=$(curl -s "$BASE_URL/$vendor/$first_mib" || echo "")

    # Extract enterprise OID
    vendor_oid=$(echo "$mib_content" | grep -m 1 -oE "(enterprises[[:space:]]+[0-9]+|1[[:space:]]+3[[:space:]]+6[[:space:]]+1[[:space:]]+4[[:space:]]+1[[:space:]]+[0-9]+)" | \
                 sed -E 's/enterprises[[:space:]]+([0-9]+)/1.3.6.1.4.1.\1/; s/1[[:space:]]+3[[:space:]]+6[[:space:]]+1[[:space:]]+4[[:space:]]+1[[:space:]]+([0-9]+)/1.3.6.1.4.1.\1/' || echo "")

    if [ -z "$vendor_oid" ]; then
        echo "  ⚠️ Could not find enterprise OID for $vendor"
        continue
    fi

    echo "  ✓ Found enterprise OID: $vendor_oid"

    # Build MIB list JSON
    mib_list=""
    while IFS= read -r mib; do
        [ -z "$mib" ] && continue
        if [ -n "$mib_list" ]; then
            mib_list+=","
        fi
        mib_list+="{\"name\":\"$mib\",\"file\":\"$mib\"}"
    done <<< "$mib_files"

    # Write to temp file
    echo "$vendor_oid|$vendor|$vendor|$mib_list" >> "$TEMP_DATA"

    echo "  ✓ Indexed: $vendor_oid with $mib_count MIBs"
done

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

# Count vendors
vendor_count=$(grep -c '"name":' "$OUTPUT_JSON" || echo "0")

echo ""
echo "✅ MIB index generated successfully!"
echo "📄 Output: $OUTPUT_JSON"
echo "📊 Vendors indexed: $vendor_count"

# Show sample for comware
if command -v jq &> /dev/null; then
    echo ""
    echo "📋 Comware entry:"
    jq '.vendors["1.3.6.1.4.1.25506"]' "$OUTPUT_JSON" 2>/dev/null || true
fi

exit 0
