#!/bin/bash

# Local CI Test Script
# This script simulates the CI integration test locally

set -e  # Exit on any error

echo "🚀 Starting Local CI Test..."
echo "================================"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Step 1: Clean and build
echo "📦 Step 1: Building project..."
make clean
make build
print_status "Build completed"

# Step 2: Test configuration files
echo "🔧 Step 2: Testing configuration files..."
echo "   Testing app.toml syntax..."

# Test TOML syntax using Python
python3 -c "
import sys
try:
    # Try to parse as TOML-like structure
    with open('docker/localnode/config/app.toml', 'r') as f:
        content = f.read()
    
    # Basic syntax checks
    if 'global-labels = [' in content and 'global-labels = []' not in content:
        print('❌ Found unclosed global-labels array')
        sys.exit(1)
    
    # Check for basic TOML structure
    lines = content.split('\n')
    in_section = False
    for i, line in enumerate(lines):
        line = line.strip()
        if line.startswith('[') and line.endswith(']'):
            in_section = True
        elif line.startswith('#') or line == '':
            continue
        elif '=' in line and not in_section and not line.startswith('#'):
            print(f'❌ Configuration item outside section at line {i+1}: {line}')
            sys.exit(1)
    
    print('✅ app.toml syntax looks good')
except Exception as e:
    print(f'❌ Error checking app.toml: {e}')
    sys.exit(1)
"

print_status "Configuration files validated"

# Step 3: Test node initialization
echo "🔧 Step 3: Testing node initialization..."
rm -rf /tmp/test-local-ci
~/go/bin/seid init test-local-ci --chain-id test-chain --home /tmp/test-local-ci >/dev/null 2>&1
print_status "Node initialization successful"

# Step 4: Test Docker cluster startup (optional, takes longer)
if [ "$1" = "--full" ]; then
    echo "🐳 Step 4: Testing Docker cluster startup..."
    print_warning "This will take several minutes..."
    
    # Start Docker cluster in background
    make clean
    INVARIANT_CHECK_INTERVAL=10 make docker-cluster-start &
    DOCKER_PID=$!
    
    # Wait for cluster to start
    echo "   Waiting for cluster to start..."
    timeout=300  # 5 minutes timeout
    elapsed=0
    while [ ! -f build/generated/launch.complete ] || [ $(cat build/generated/launch.complete | wc -l) -lt 4 ]; do
        if [ $elapsed -ge $timeout ]; then
            print_error "Docker cluster startup timeout"
            kill $DOCKER_PID 2>/dev/null || true
            make docker-cluster-stop
            exit 1
        fi
        sleep 10
        elapsed=$((elapsed + 10))
        echo "   Still waiting... (${elapsed}s elapsed)"
    done
    
    print_status "Docker cluster started successfully"
    
    # Test the startup verification
    echo "🔍 Step 5: Testing startup verification..."
    python3 integration_test/scripts/runner.py integration_test/startup/startup_test.yaml
    print_status "Startup verification passed"
    
    # Clean up
    echo "🧹 Cleaning up Docker cluster..."
    make docker-cluster-stop
    print_status "Docker cluster stopped"
else
    echo "⏭️  Step 4: Skipping Docker cluster test (use --full to enable)"
    print_warning "Use './test_local_ci.sh --full' to test Docker cluster startup"
fi

echo ""
echo "🎉 Local CI Test Completed Successfully!"
echo "================================"
echo "✅ All tests passed"
echo "✅ Configuration files are valid"
echo "✅ Node initialization works"
if [ "$1" = "--full" ]; then
    echo "✅ Docker cluster startup works"
    echo "✅ Startup verification works"
fi
echo ""
echo "🚀 Ready to push to CI!"
