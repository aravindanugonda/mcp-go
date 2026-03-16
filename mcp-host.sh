#!/usr/bin/env bash
# mcp-host.sh — start / stop / restart / status / logs

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PID_FILE="$SCRIPT_DIR/.mcp-host.pid"
LOG_FILE="$SCRIPT_DIR/.mcp-host.log"
GO_BIN="${GO_BIN:-$(command -v go 2>/dev/null || echo "$HOME/go/bin/go")}"
CONFIG="${CONFIG:-$SCRIPT_DIR/config.yaml}"

# ── helpers ──────────────────────────────────────────────────────────────────
red()   { printf '\033[0;31m%s\033[0m\n' "$*"; }
green() { printf '\033[0;32m%s\033[0m\n' "$*"; }
blue()  { printf '\033[0;34m%s\033[0m\n' "$*"; }
bold()  { printf '\033[1m%s\033[0m\n'    "$*"; }

is_running() {
    [[ -f "$PID_FILE" ]] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null
}

# ── commands ─────────────────────────────────────────────────────────────────
cmd_start() {
    if is_running; then
        green "Already running  (PID $(cat "$PID_FILE"))"
        return
    fi

    blue "Building…"
    cd "$SCRIPT_DIR"
    "$GO_BIN" build -o .mcp-host-bin ./cmd/main.go

    blue "Starting MCP Host…"
    # Use setsid so the process gets its own process group — lets us kill
    # the entire tree (Go binary + npx MCP child) with a single signal.
    setsid nohup "$SCRIPT_DIR/.mcp-host-bin" -config "$CONFIG" \
        >> "$LOG_FILE" 2>&1 &
    echo $! > "$PID_FILE"

    # Wait a moment and confirm it's still alive
    sleep 1
    if is_running; then
        green "Started  (PID $(cat "$PID_FILE"))  —  logs: $LOG_FILE"
        # Print the listening address from the log
        url=$(grep -m1 "Open http" "$LOG_FILE" 2>/dev/null | sed 's/^.*Open //')
        [[ -n "$url" ]] && green "  $url" || true
    else
        red "Process exited immediately — check logs:"
        tail -20 "$LOG_FILE"
        rm -f "$PID_FILE"
        exit 1
    fi
}

cmd_stop() {
    if ! is_running; then
        bold "Not running."
        return
    fi
    local pid pgid
    pid=$(cat "$PID_FILE")
    # Get the process-group ID so we can kill the whole tree
    # (Go binary + its npx MCP child processes)
    pgid=$(ps -o pgid= -p "$pid" 2>/dev/null | tr -d ' ')

    rm -f "$PID_FILE"

    if [[ -n "$pgid" && "$pgid" != "0" ]]; then
        kill -TERM -- "-$pgid" 2>/dev/null || true
    else
        kill -TERM "$pid" 2>/dev/null || true
    fi

    # Wait up to 5 s for a clean exit
    for _ in {1..10}; do
        kill -0 "$pid" 2>/dev/null || break
        sleep 0.5
    done

    # Force-kill if still alive
    if kill -0 "$pid" 2>/dev/null; then
        if [[ -n "$pgid" && "$pgid" != "0" ]]; then
            kill -9 -- "-$pgid" 2>/dev/null || true
        else
            kill -9 "$pid" 2>/dev/null || true
        fi
    fi

    # Belt-and-suspenders: clean up any stray npx MCP server processes
    pkill -f "server-filesystem" 2>/dev/null || true

    green "Stopped  (PID $pid)"
}

cmd_restart() {
    cmd_stop
    sleep 0.5
    cmd_start
}

cmd_status() {
    if is_running; then
        green "Running  (PID $(cat "$PID_FILE"))"
        # Quick health check
        local port
        port=$(grep -oP '(?<=port: )\d+' "$CONFIG" 2>/dev/null || echo "8080")
        if curl -sf "http://localhost:${port}/api/status" > /dev/null 2>&1; then
            green "  HTTP responding on port $port"
            curl -s "http://localhost:${port}/api/status" | python3 -c \
                "import sys,json; s=json.load(sys.stdin)['mcp']; [print(f'  MCP {k}: {\"connected\" if v else \"disconnected\"}') for k,v in s.items()]" 2>/dev/null || true
        else
            red "  HTTP not yet responding on port $port"
        fi
    else
        bold "Not running."
    fi
}

cmd_logs() {
    if [[ ! -f "$LOG_FILE" ]]; then
        bold "No log file found at $LOG_FILE"
        return
    fi
    local lines="${1:-50}"
    blue "Last $lines lines of $LOG_FILE  (Ctrl-C to exit):"
    tail -n "$lines" -f "$LOG_FILE"
}

cmd_help() {
    bold "Usage: ./mcp-host.sh <command> [options]"
    echo ""
    echo "  start      Build and start the server in the background"
    echo "  stop       Stop the running server"
    echo "  restart    Stop then start"
    echo "  status     Show running state and MCP connection health"
    echo "  logs [N]   Tail the log file (default last 50 lines, follows)"
    echo ""
    echo "Environment variables:"
    echo "  GO_BIN     Path to the go binary  (default: auto-detected)"
    echo "  CONFIG     Path to config.yaml    (default: ./config.yaml)"
}

# ── dispatch ─────────────────────────────────────────────────────────────────
case "${1:-help}" in
    start)   cmd_start   ;;
    stop)    cmd_stop    ;;
    restart) cmd_restart ;;
    status)  cmd_status  ;;
    logs)    cmd_logs "${2:-50}" ;;
    *)       cmd_help    ;;
esac
