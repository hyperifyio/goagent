#!/usr/bin/env bash
set -euo pipefail

mkdir -p reports/tests /tmp/gobin
export GOBIN=/tmp/gobin
export PATH="$GOBIN:$PATH"

# Install gotestsum deterministically in the container
GO111MODULE=on go install gotest.tools/gotestsum@v1.12.0

# Run full test suite with coverage; emit JUnit and HTML coverage under reports/tests
# Use -count=1 to avoid cache, -covermode=atomic for accuracy in parallel tests
gotestsum \
  --format standard-verbose \
  --junitfile reports/tests/junit.xml \
  -- \
  -count=1 -covermode=atomic -coverprofile=reports/tests/coverage.out ./...

# Generate HTML coverage report
go tool cover -html=reports/tests/coverage.out -o reports/tests/coverage.html

echo "JUnit: reports/tests/junit.xml"
echo "Coverage HTML: reports/tests/coverage.html"