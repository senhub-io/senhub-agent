#!/bin/bash

# Script to verify documentation links and structure
# Usage: ./scripts/verify-docs.sh

echo "🔍 Verifying SenHub Agent Documentation Structure..."
echo

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

errors=0
checks=0

check_file() {
    local file="$1"
    local context="$2"
    checks=$((checks + 1))
    
    if [ -f "$file" ]; then
        echo -e "${GREEN}✓${NC} $context: $file"
    else
        echo -e "${RED}✗${NC} $context: $file (MISSING)"
        errors=$((errors + 1))
    fi
}

echo "📁 Checking documentation structure..."

# Main documentation portal
check_file "docs/README.md" "Main portal"

# User Guide
check_file "docs/user-guide/README.md" "User guide index"
check_file "docs/user-guide/QUICK-START-OFFLINE.md" "Quick start guide"
check_file "docs/user-guide/OFFLINE-MODE.md" "Offline mode guide"
check_file "docs/user-guide/PROBE-CONFIGURATION.md" "Probe configuration"

# Admin Guide
check_file "docs/admin-guide/README.md" "Admin guide index"
check_file "docs/admin-guide/HTTP-STRATEGY.md" "HTTP strategy"
check_file "docs/admin-guide/HTTPS-CONFIGURATION.md" "HTTPS configuration"
check_file "docs/admin-guide/HTTP-BIND-ADDRESS.md" "HTTP bind address"
check_file "docs/admin-guide/LOGGING.md" "Logging guide"

# Technical Reference
check_file "docs/technical-reference/README.md" "Technical reference index"
check_file "docs/technical-reference/README-REDFISH.md" "Redfish overview"
check_file "docs/technical-reference/REDFISH-METRICS.md" "Redfish metrics"
check_file "docs/technical-reference/REDFISH-TAGS.md" "Redfish tags"
check_file "docs/technical-reference/OTEL-METRICS.md" "OTEL metrics"
check_file "docs/technical-reference/OTEL-PROBE.md" "OTEL probe"

# Troubleshooting
check_file "docs/troubleshooting/README.md" "Troubleshooting index"
check_file "docs/troubleshooting/TROUBLESHOOTING-OFFLINE.md" "Offline troubleshooting"

# Legacy files
check_file "DOCUMENTATION-INDEX.md" "Legacy documentation index"
check_file "README.markdown" "Main README"
check_file "CLAUDE.md" "Development notes"

echo
echo "📊 Documentation Verification Summary:"
echo "   Total checks: $checks"
echo "   Files found: $((checks - errors))"
if [ $errors -eq 0 ]; then
    echo -e "   ${GREEN}✓ All documentation files present!${NC}"
    echo
    echo "🚀 Quick links:"
    echo "   📖 Start here: docs/README.md"
    echo "   👤 User guide: docs/user-guide/"
    echo "   ⚙️ Admin guide: docs/admin-guide/"
    echo "   🔧 Technical reference: docs/technical-reference/"
    echo "   🚨 Troubleshooting: docs/troubleshooting/"
else
    echo -e "   ${RED}✗ Missing files: $errors${NC}"
    exit 1
fi

echo
echo "✨ Documentation structure verification complete!"