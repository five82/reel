#!/bin/bash
# Dependency health check for reel.
# Reports available Go module updates, reachable vulnerabilities, and newer CI action tags without changing files.

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

is_version_tag() {
    [[ "$1" =~ ^v?[0-9]+([.][0-9]+)*$ ]]
}

is_major_version_tag() {
    [[ "$1" =~ ^v?[0-9]+$ ]]
}

version_major() {
    local version=${1#v}
    echo "${version%%.*}"
}

latest_stable_tag() {
    local repo=$1
    local tag_output

    if ! tag_output=$(git ls-remote --tags --refs "$repo" 2>/dev/null); then
        return 1
    fi

    printf '%s\n' "$tag_output" |
        awk -F 'refs/tags/' 'NF > 1 {print $2}' |
        grep -E '^v?[0-9]+([.][0-9]+)*$' |
        sort -V |
        tail -n 1 || true
}

is_newer_version_available() {
    local current=$1
    local latest=$2
    local highest

    if [ -z "$latest" ] || ! is_version_tag "$current"; then
        return 1
    fi

    if is_major_version_tag "$current"; then
        [ "$(version_major "$latest")" -gt "$(version_major "$current")" ]
        return
    fi

    highest=$(printf '%s\n%s\n' "$current" "$latest" | sort -V | tail -n 1)
    [ "$highest" = "$latest" ] && [ "$current" != "$latest" ]
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
    print_step "Checking Forgejo Actions dependencies"
    ACTION_REFS=$(grep -RhoE 'uses:[[:space:]]*[^[:space:]]+' .forgejo/workflows 2>/dev/null | sed 's/^uses:[[:space:]]*//' | sort -u || true)
    if [ -n "$ACTION_REFS" ]; then
        if ! command -v git &>/dev/null; then
            printf '%s\n' "$ACTION_REFS" | sed 's/^/   /'
            echo
            print_warning "Git is not installed; skipping action version checks."
        else
            OUTDATED_ACTIONS=0

            while IFS= read -r action_ref; do
                repo=${action_ref%@*}
                current_ref=${action_ref##*@}

                echo "   $action_ref"

                if [[ "$action_ref" != *@* ]]; then
                    echo "      no @ref found; skipping version check"
                    continue
                fi

                if ! latest=$(latest_stable_tag "$repo"); then
                    echo "      could not reach action tags; skipping version check"
                    continue
                fi

                if [ -z "$latest" ]; then
                    echo "      no stable version tags found; skipping version check"
                    continue
                fi

                if is_newer_version_available "$current_ref" "$latest"; then
                    echo "      latest stable tag: $latest; newer version may be available"
                    OUTDATED_ACTIONS=1
                elif is_major_version_tag "$current_ref" && [ "$(version_major "$current_ref")" = "$(version_major "$latest")" ]; then
                    echo "      latest stable tag: $latest; $current_ref tracks the latest major"
                elif is_version_tag "$current_ref"; then
                    echo "      latest stable tag: $latest; no newer stable tag found"
                else
                    echo "      latest stable tag: $latest; current ref is not a version tag"
                fi
            done <<< "$ACTION_REFS"

            if [ "$OUTDATED_ACTIONS" -eq 1 ]; then
                echo
                print_warning "Newer CI action tags may be available; review before updating."
            fi
        fi
    else
        print_success "No action dependencies found"
    fi
fi

echo -e "\n${GREEN}Dependency check complete${NC}"
