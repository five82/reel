#!/bin/bash
# Dependency health check for reel.
# Reports available Go module updates and reachable vulnerabilities without changing files.

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[0;33m'
NC='\033[0m'

print_step() {
    echo -e "\n${BLUE}:: $1${NC}"
}

print_success() {
    echo -e "${GREEN}   $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}   $1${NC}"
}

print_error() {
    echo -e "${RED}   $1${NC}"
}

if ! command -v go &>/dev/null; then
    print_error "Go is not installed."
    exit 1
fi

print_step "Checking for available Go module updates"
UPDATE_OUTPUT=$(go list -m -u all)
OUTDATED_OUTPUT=$(
    while IFS= read -r line; do
        if [[ "$line" != *"["* ]]; then
            continue
        fi

        module=$(awk '{print $1}' <<< "$line")
        if go mod why -m "$module" 2>/dev/null | grep -q "main module does not need module"; then
            continue
        fi

        echo "$line"
    done <<< "$UPDATE_OUTPUT"
)

if [ -n "$OUTDATED_OUTPUT" ]; then
    print_warning "Updates are available:"
    printf '%s\n' "$OUTDATED_OUTPUT" | sed 's/^/   /'
    echo
    echo "   To apply a listed update, run:"
    echo "     go get <module>@latest"
    echo "     go mod tidy"
    echo "     ./check-ci.sh"
else
    print_success "Go modules are up to date"
fi

print_step "Checking for reachable Go vulnerabilities"
if ! command -v govulncheck &>/dev/null; then
    echo "   Installing govulncheck..."
    go install golang.org/x/vuln/cmd/govulncheck@latest
fi

if govulncheck ./...; then
    print_success "No reachable vulnerabilities found"
else
    print_error "Reachable vulnerabilities detected"
    exit 1
fi

if [ -d .forgejo/workflows ]; then
    print_step "Listing Forgejo Actions dependencies"
    ACTION_REFS=$(grep -RhoE 'uses:[[:space:]]*[^[:space:]]+' .forgejo/workflows 2>/dev/null | sed 's/^uses:[[:space:]]*//' | sort -u || true)
    if [ -n "$ACTION_REFS" ]; then
        printf '%s\n' "$ACTION_REFS" | sed 's/^/   /'
        echo
        echo "   Review these manually when updating CI actions."
    else
        print_success "No action dependencies found"
    fi
fi

echo -e "\n${GREEN}Dependency check complete${NC}"
