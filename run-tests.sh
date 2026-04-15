#!/usr/bin/env bash
set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HUB_DIR="$(dirname "$SCRIPT_DIR")/stellar-hub"
DEV_PID=""
DEV_URL="${STELLAR_DEV_URL:-http://localhost:3000}"
PROD_URL="https://stellar-hub.vercel.app"

cleanup() {
    if [ -n "$DEV_PID" ]; then
        echo -e "${YELLOW}Stopping dev server (PID: $DEV_PID)...${NC}"
        kill $DEV_PID 2>/dev/null || true
        wait $DEV_PID 2>/dev/null || true
    fi
}
trap cleanup EXIT

start_dev_server() {
    if curl -s "$DEV_URL/api/themes" > /dev/null 2>&1; then
        echo -e "${GREEN}Dev server already running at $DEV_URL${NC}"
        return 0
    fi

    echo -e "${BLUE}Starting stellar-hub dev server...${NC}"
    cd "$HUB_DIR"
    pnpm dev > /dev/null 2>&1 &
    DEV_PID=$!
    cd "$SCRIPT_DIR"

    echo -n "Waiting for server"
    for i in {1..30}; do
        if curl -s "$DEV_URL/api/themes" > /dev/null 2>&1; then
            echo -e " ${GREEN}ready!${NC}"
            return 0
        fi
        echo -n "."
        sleep 1
    done
    echo -e " ${RED}timeout${NC}"
    return 1
}

# Run go tests with colored output and summary
run_go_tests() {
    local tmp_file
    tmp_file=$(mktemp)
    local passed=0
    local failed=0
    local failed_tests=()
    local current_pkg=""

    # Run go test with JSON output
    if command -v nix &> /dev/null && [ -f "$SCRIPT_DIR/flake.nix" ]; then
        nix develop "$SCRIPT_DIR" --command go test -json "$@" 2>/dev/null | grep '^{' > "$tmp_file" || true
    else
        go test -json "$@" 2>&1 | grep '^{' > "$tmp_file" || true
    fi

    # Parse JSON output
    local parsed_file
    parsed_file=$(mktemp)
    jq -r 'select(.Action == "pass" or .Action == "fail") | select(.Test) | "\(.Action) \(.Package) \(.Test)"' "$tmp_file" > "$parsed_file" 2>/dev/null

    while read -r action pkg test; do
        if [ -n "$pkg" ] && [ "$pkg" != "$current_pkg" ]; then
            if [ -n "$current_pkg" ]; then
                echo ""
            fi
            current_pkg="$pkg"
            echo -e "${CYAN}$pkg${NC}"
        fi

        case "$action" in
            pass)
                echo -e "  ${GREEN}PASS${NC}  $test"
                ((passed++))
                ;;
            fail)
                echo -e "  ${RED}FAIL${NC}  $test"
                ((failed++))
                failed_tests+=("$pkg $test")
                ;;
        esac
    done < "$parsed_file"

    rm -f "$parsed_file"
    rm -f "$tmp_file"

    echo ""
    echo -e "${BLUE}=== Summary ===${NC}"
    echo -e "Passed: ${GREEN}$passed${NC}"
    echo -e "Failed: ${RED}$failed${NC}"

    if [ ${#failed_tests[@]} -gt 0 ]; then
        echo ""
        echo -e "${RED}Failed tests:${NC}"
        for ft in "${failed_tests[@]}"; do
            echo -e "  - $ft"
        done
    fi

    [ $failed -eq 0 ]
}

run_e2e_mock() {
    echo -e "\n${BLUE}=== E2E Tests (Mock) ===${NC}"
    echo -e "${CYAN}Fast tests against mock API servers.${NC}\n"
    cd "$SCRIPT_DIR"

    run_go_tests -v ./cmd/...
}

run_e2e_local() {
    echo -e "\n${BLUE}=== E2E Tests (Local) ===${NC}"
    echo -e "${CYAN}Tests against local stellar-hub dev server.${NC}\n"

    start_dev_server || return 1
    cd "$SCRIPT_DIR"

    STELLAR_DEV_URL="$DEV_URL" run_go_tests -tags=integration -v ./internal/api/...
}

run_e2e_prod() {
    echo -e "\n${BLUE}=== E2E Tests (Production) ===${NC}"
    echo -e "${CYAN}Tests against stellar-hub.vercel.app.${NC}\n"
    cd "$SCRIPT_DIR"

    STELLAR_DEV_URL="$PROD_URL" run_go_tests -tags=integration -v ./internal/api/...
}

run_unit_tests() {
    echo -e "\n${BLUE}=== Unit Tests ===${NC}"
    echo -e "${CYAN}Internal module tests.${NC}\n"
    cd "$SCRIPT_DIR"

    run_go_tests -v ./internal/...
}

run_all_tests() {
    run_e2e_mock
    run_unit_tests
    echo -e "\n${GREEN}All tests passed!${NC}"
}

run_lint() {
    echo -e "\n${BLUE}=== Running golangci-lint ===${NC}"
    cd "$SCRIPT_DIR"

    if command -v nix &> /dev/null && [ -f "$SCRIPT_DIR/flake.nix" ]; then
        nix develop "$SCRIPT_DIR" --command golangci-lint run
    else
        golangci-lint run
    fi
}

show_menu() {
    echo -e "\n${GREEN}+------------------------------------+${NC}"
    echo -e "${GREEN}|     Stellar Test Runner            |${NC}"
    echo -e "${GREEN}+------------------------------------+${NC}"
    echo ""
    echo "  1) E2E tests (mock)        - Fast, no server needed"
    echo "  2) E2E tests (local)       - Requires local dev server"
    echo "  3) E2E tests (production)  - Tests against stellar-hub.vercel.app"
    echo "  4) Unit tests              - Internal module tests"
    echo "  5) All tests"
    echo "  6) Run golangci-lint"
    echo "  q) Quit"
    echo ""
}

show_help() {
    echo "Usage: $0 [option]"
    echo ""
    echo "Options:"
    echo "  -e, --e2e           Run E2E tests (mock mode)"
    echo "  -el, --e2e-local    Run E2E tests against local dev server"
    echo "  -ep, --e2e-prod     Run E2E tests against production"
    echo "  -u, --unit          Run unit tests"
    echo "  -a, --all           Run all tests"
    echo "  -l, --lint          Run golangci-lint"
    echo "  -h, --help          Show this help"
    echo ""
    echo "Without options, runs interactive menu."
    echo ""
    echo "Environment variables:"
    echo "  STELLAR_DEV_URL    Dev server URL (default: http://localhost:3000)"
}

# Parse command line flags
case "${1:-}" in
    --e2e|-e)
        run_e2e_mock
        exit $?
        ;;
    --e2e-local|-el)
        run_e2e_local
        exit $?
        ;;
    --e2e-prod|-ep)
        run_e2e_prod
        exit $?
        ;;
    --unit|-u)
        run_unit_tests
        exit $?
        ;;
    --all|-a)
        run_all_tests
        exit $?
        ;;
    --lint|-l)
        run_lint
        exit $?
        ;;
    --help|-h)
        show_help
        exit 0
        ;;
    "")
        # Interactive mode
        ;;
    *)
        echo -e "${RED}Unknown option: $1${NC}"
        show_help
        exit 1
        ;;
esac

# Interactive mode
while true; do
    show_menu
    read -p "Select option: " choice
    case $choice in
        1) run_e2e_mock ;;
        2) run_e2e_local ;;
        3) run_e2e_prod ;;
        4) run_unit_tests ;;
        5) run_all_tests ;;
        6) run_lint ;;
        q|Q) echo "Bye!"; exit 0 ;;
        *) echo -e "${RED}Invalid option${NC}" ;;
    esac
    echo -e "\n${YELLOW}Press Enter to continue...${NC}"
    read
done
