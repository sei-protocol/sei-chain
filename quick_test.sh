#!/bin/bash

# Quick Test Script - Fast validation before CI
# This script runs the most important checks quickly

set -e

echo "⚡ Quick Test - Fast CI Validation"
echo "=================================="

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

print_status() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

# Test 1: Configuration syntax
echo "🔧 Testing configuration syntax..."
python3 -c "
import sys
try:
    with open('docker/localnode/config/app.toml', 'r') as f:
        content = f.read()
    
    # Check for common syntax issues
    issues = []
    
    # Note: Removed strict global-labels check - multiline empty arrays are valid TOML
    
    # Check for duplicate sections
    lines = content.split('\n')
    sections = []
    for line in lines:
        if line.strip().startswith('[') and line.strip().endswith(']'):
            sections.append(line.strip())
    
    duplicates = [s for s in set(sections) if sections.count(s) > 1]
    if duplicates:
        issues.append(f'Duplicate sections: {duplicates}')
    
    # Note: Removed check for config items outside sections
    # TOML standard allows top-level config items before the first explicit section
    
    if issues:
        print('❌ Configuration issues found:')
        for issue in issues:
            print(f'   - {issue}')
        sys.exit(1)
    else:
        print('✅ Configuration syntax is valid')
        
except Exception as e:
    print(f'❌ Error checking configuration: {e}')
    sys.exit(1)
"

# Test 2: Node initialization
echo "🔧 Testing node initialization..."
rm -rf /tmp/quick-test
~/go/bin/seid init quick-test --chain-id test-chain --home /tmp/quick-test >/dev/null 2>&1
print_status "Node initialization works"

# Test 3: Build test
echo "🔧 Testing build..."
make build >/dev/null 2>&1
print_status "Build successful"

# Test 4: Check if seid binary works
echo "🔧 Testing seid binary..."
~/go/bin/seid version --home /tmp/quick-test >/dev/null 2>&1
print_status "seid binary works"

echo ""
echo "🎉 Quick Test Passed!"
echo "===================="
echo "✅ Configuration syntax valid"
echo "✅ Node initialization works"
echo "✅ Build successful"
echo "✅ seid binary works"
echo ""
echo "🚀 Safe to push to CI!"
echo ""
echo "💡 For full Docker cluster test, run: ./test_local_ci.sh --full"
