#!/usr/bin/env bash
# mcp-host.sh — start / stop / restart / status / logs

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PID_FILE="$SCRIPT_DIR/.mcp-host.pid"
LOG_FILE="$SCRIPT_DIR/.mcp-host.log"
GO_BIN="${GO_BIN:-$(command -v go 2>/dev/null || echo "$HOME/go/bin/go")}"
CONFIG="${CONFIG:-$SCRIPT_DIR/config.yaml}"

# Reranker (optional) — Python cross-encoder served via Flask
RERANKER_SCRIPT="${RERANKER_SCRIPT:-$SCRIPT_DIR/../mcp-pinecone-rag/reranker.py}"
RERANKER_PID_FILE="$SCRIPT_DIR/.reranker.pid"
RERANKER_LOG_FILE="$SCRIPT_DIR/.reranker.log"
RERANKER_PORT="${RERANKER_PORT:-8090}"

# ── helpers ──────────────────────────────────────────────────────────────────
red()   { printf '\033[0;31m%s\033[0m\n' "$*"; }
green() { printf '\033[0;32m%s\033[0m\n' "$*"; }
blue()  { printf '\033[0;34m%s\033[0m\n' "$*"; }
bold()  { printf '\033[1m%s\033[0m\n'    "$*"; }

is_running() {
    [[ -f "$PID_FILE" ]] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null
}

reranker_running() {
    [[ -f "$RERANKER_PID_FILE" ]] && kill -0 "$(cat "$RERANKER_PID_FILE")" 2>/dev/null
}

start_reranker() {
    if [[ ! -f "$RERANKER_SCRIPT" ]]; then
        blue "Reranker script not found at $RERANKER_SCRIPT — skipping"
        blue "  Override: RERANKER_SCRIPT=/path/to/reranker.py ./mcp-host.sh start"
        return
    fi
    if reranker_running; then
        green "Reranker already running  (PID $(cat "$RERANKER_PID_FILE"))"
        return
    fi
    if ! command -v python3 >/dev/null 2>&1; then
        red "python3 not found — reranker disabled"
        return
    fi

    local venv_dir python pip
    venv_dir="$(dirname "$RERANKER_SCRIPT")/.venv"
    python="$venv_dir/bin/python3"
    pip="$venv_dir/bin/pip"

    # Create venv once (handles externally-managed-environment restriction)
    if [[ ! -f "$python" ]]; then
        blue "Creating Python venv at $venv_dir …"
        if ! python3 -m venv "$venv_dir"; then
            red "Failed to create venv — try: apt install python3-full python3-venv"
            return
        fi
    fi

    # Install deps once — skip if already present
    if ! "$python" -c "import fastembed, flask" 2>/dev/null; then
        blue "Installing reranker dependencies into venv (one-time, ~1 min)…"
        if ! "$pip" install --quiet --no-cache-dir fastembed flask; then
            red "pip install failed — check: $RERANKER_LOG_FILE"
            return
        fi
        green "Dependencies installed."
    fi

    blue "Starting reranker (port $RERANKER_PORT)…"
    RERANKER_PORT="$RERANKER_PORT" setsid nohup "$python" "$RERANKER_SCRIPT" \
        >> "$RERANKER_LOG_FILE" 2>&1 &
    echo $! > "$RERANKER_PID_FILE"

    sleep 1
    if reranker_running; then
        green "Reranker started  (PID $(cat "$RERANKER_PID_FILE"))"
        green "  Model loading in background — may take ~30s on first run"
        green "  Logs: $RERANKER_LOG_FILE"
        green "  Add to config.yaml pinecone-rag env:  RERANKER_URL: http://localhost:$RERANKER_PORT"
    else
        red "Reranker exited immediately — check: $RERANKER_LOG_FILE"
        rm -f "$RERANKER_PID_FILE"
    fi
}

stop_reranker() {
    if ! reranker_running; then
        return
    fi
    local pid pgid
    pid=$(cat "$RERANKER_PID_FILE")
    pgid=$(ps -o pgid= -p "$pid" 2>/dev/null | tr -d ' ')
    rm -f "$RERANKER_PID_FILE"
    if [[ -n "$pgid" && "$pgid" != "0" ]]; then
        kill -TERM -- "-$pgid" 2>/dev/null || true
    else
        kill -TERM "$pid" 2>/dev/null || true
    fi
    green "Reranker stopped  (PID $pid)"
}

# ── commands ─────────────────────────────────────────────────────────────────
cmd_start() {
    if is_running; then
        green "Already running  (PID $(cat "$PID_FILE"))"
        return
    fi

    start_reranker

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
    stop_reranker
}

cmd_restart() {
    cmd_stop
    sleep 0.5
    cmd_start
}

cmd_status() {
    if is_running; then
        green "Host:     running  (PID $(cat "$PID_FILE"))"
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
        bold "Host:     not running"
    fi

    if reranker_running; then
        green "Reranker: running  (PID $(cat "$RERANKER_PID_FILE"), port $RERANKER_PORT)"
        if curl -sf "http://localhost:${RERANKER_PORT}/health" > /dev/null 2>&1; then
            green "  /health OK — model loaded and ready"
        else
            blue "  /health not yet responding — model may still be loading"
        fi
    else
        bold "Reranker: not running  (start with: ./mcp-host.sh start)"
    fi
}

cmd_logs() {
    local target="${1:-host}"
    local lines="${2:-50}"

    if [[ "$target" == "reranker" ]]; then
        if [[ ! -f "$RERANKER_LOG_FILE" ]]; then
            bold "No reranker log file found at $RERANKER_LOG_FILE"
            return
        fi
        blue "Last $lines lines of $RERANKER_LOG_FILE  (Ctrl-C to exit):"
        tail -n "$lines" -f "$RERANKER_LOG_FILE"
    else
        # legacy: first arg might be a number (./mcp-host.sh logs 100)
        if [[ "$target" =~ ^[0-9]+$ ]]; then
            lines="$target"
        fi
        if [[ ! -f "$LOG_FILE" ]]; then
            bold "No log file found at $LOG_FILE"
            return
        fi
        blue "Last $lines lines of $LOG_FILE  (Ctrl-C to exit):"
        tail -n "$lines" -f "$LOG_FILE"
    fi
}

cmd_help() {
    bold "Usage: ./mcp-host.sh <command> [options]"
    echo ""
    echo "  start                Build and start host + reranker"
    echo "  stop                 Stop host + reranker"
    echo "  restart              Stop then start"
    echo "  status               Show running state, MCP health, and reranker health"
    echo "  logs [N]             Tail host log        (default: last 50 lines, follows)"
    echo "  logs reranker [N]    Tail reranker log"
    echo ""
    echo "Environment variables:"
    echo "  GO_BIN             Path to the go binary         (default: auto-detected)"
    echo "  CONFIG             Path to config.yaml           (default: ./config.yaml)"
    echo "  RERANKER_SCRIPT    Path to reranker.py           (default: ../mcp-pinecone-rag/reranker.py)"
    echo "  RERANKER_PORT      Port for the reranker server  (default: 8090)"
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
