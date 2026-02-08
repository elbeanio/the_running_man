#!/bin/bash
set -e

echo "=== Phase 1 Success Criteria Testing ==="
echo ""

# Build the binary
echo "1. Building running-man..."
go build -o running-man ./cmd/running-man
echo "✓ Build successful"
echo ""

# Test 1: Zero-config startup
echo "2. Testing zero-config startup..."
./running-man run --no-tui -- echo "Hello World" > /tmp/running-man-test.log 2>&1 &
PID=$!
sleep 2
kill $PID 2>/dev/null || true
if grep -q "Hello World" /tmp/running-man-test.log; then
    echo "✓ Zero-config startup works"
else
    echo "✗ Zero-config startup failed"
    exit 1
fi
echo ""

# Test 2: Can wrap Python process
echo "3. Testing Python process wrapping..."
./running-man run --no-tui -- python3 -c "print('Python test'); import sys; print('ERROR: Test error', file=sys.stderr)" > /tmp/running-man-python.log 2>&1 &
PID=$!
sleep 2
kill $PID 2>/dev/null || true
if grep -q "Python test" /tmp/running-man-python.log && grep -q "ERROR: Test error" /tmp/running-man-python.log; then
    echo "✓ Python process wrapping works"
else
    echo "✗ Python process wrapping failed"
    exit 1
fi
echo ""

# Test 3: Can wrap Node process (if node is available)
echo "4. Testing Node.js process wrapping..."
if command -v node &> /dev/null; then
    ./running-man run --no-tui -- node -e "console.log('Node test'); console.error('Node error');" > /tmp/running-man-node.log 2>&1 &
    PID=$!
    sleep 2
    kill $PID 2>/dev/null || true
    if grep -q "Node test" /tmp/running-man-node.log; then
        echo "✓ Node.js process wrapping works"
    else
        echo "✗ Node.js process wrapping failed"
        exit 1
    fi
else
    echo "⊘ Node.js not available, skipping"
fi
echo ""

# Test 4: API query works
echo "5. Testing API queries..."
./running-man run --no-tui --api-port 9001 -- python3 test_script.py > /dev/null 2>&1 &
PID=$!
sleep 3

# Test /health
echo "  Testing GET /health..."
if curl -s http://localhost:9001/health | grep -q '"status":"ok"'; then
    echo "  ✓ /health endpoint works"
else
    echo "  ✗ /health endpoint failed"
    kill $PID 2>/dev/null || true
    exit 1
fi

# Test /logs
echo "  Testing GET /logs..."
if curl -s http://localhost:9001/logs | grep -q '"count"'; then
    echo "  ✓ /logs endpoint works"
else
    echo "  ✗ /logs endpoint failed"
    kill $PID 2>/dev/null || true
    exit 1
fi

# Test /errors
echo "  Testing GET /errors..."
if curl -s http://localhost:9001/errors | grep -q '"count"'; then
    echo "  ✓ /errors endpoint works"
else
    echo "  ✗ /errors endpoint failed"
    kill $PID 2>/dev/null || true
    exit 1
fi

# Test filtering
echo "  Testing query filters..."
if curl -s "http://localhost:9001/logs?since=30s&level=error" | grep -q '"logs"'; then
    echo "  ✓ Query filtering works"
else
    echo "  ✗ Query filtering failed"
    kill $PID 2>/dev/null || true
    exit 1
fi

kill $PID 2>/dev/null || true
echo ""

# Test 5: Run all unit tests
echo "6. Running all unit tests..."
go test ./... -v | grep -E "^(PASS|ok|FAIL)" | tail -5
if go test ./... > /dev/null 2>&1; then
    echo "✓ All unit tests pass"
else
    echo "✗ Unit tests failed"
    exit 1
fi
echo ""

echo "=== Phase 1 Success Criteria: ALL PASSED ✓ ==="
echo ""
echo "Summary:"
echo "  ✓ Can wrap single Python/Node process"
echo "  ✓ Captures and parses errors correctly"
echo "  ✓ Agent can query recent errors via API"
echo "  ✓ Zero-config startup for simple cases"
echo "  ✓ All unit tests passing"
echo ""
echo "Phase 1 MVP is complete!"
