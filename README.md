# mcp-go

A Go-based **MCP Host** — a chatbot application that connects [Ollama](https://ollama.com) LLMs to tools exposed by [Model Context Protocol (MCP)](https://modelcontextprotocol.io) servers, with a ChatGPT-style web interface.

---

## Features

- **Ollama integration** — works with any model served by Ollama that supports tool calling
- **MCP server support** — connects to any MCP server via `stdio` (subprocess) or `http` transport
- **Tool calling loop** — automatically calls tools and feeds results back to the model, up to 5 iterations
- **ChatGPT-style UI** — dark theme with markdown rendering and syntax-highlighted, copyable code blocks
- **Real-time chat** — WebSocket with automatic REST fallback
- **Shell script** — one-command start, stop, restart, status, and log tailing for the host **and** the local reranker

---

## Prerequisites

| Requirement | Notes |
|---|---|
| [Go 1.21+](https://go.dev/dl/) | Build the server |
| [Ollama](https://ollama.com) | Running locally on port `11434` |
| [Node.js / npx](https://nodejs.org) | Required for stdio MCP servers (e.g. `@modelcontextprotocol/server-filesystem`) |
| Python 3 + `python3-venv` | Optional — only needed if using the local reranker |

---

## Quick Start

### 1. Clone

```bash
git clone https://github.com/aravindanugonda/mcp-go.git
cd mcp-go
```

### 2. Configure

Edit `config.yaml` to point at your Ollama model and set the directories the filesystem MCP server can access:

```yaml
host: "0.0.0.0"
port: 8080

ollama:
  base_url: "http://localhost:11434"
  model: "minimax-m2.5:cloud"   # any tool-capable Ollama model

mcp_servers:
  - name: "filesystem"
    type: "stdio"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp", "/home/user/projects"]
    env: {}
```

### 3. Start

```bash
chmod +x mcp-host.sh
./mcp-host.sh start
```

Open **http://localhost:8080** in your browser.

---

## Shell Script

```bash
./mcp-host.sh start              # Build and start host + reranker (if configured)
./mcp-host.sh stop               # Stop host + reranker
./mcp-host.sh restart            # Stop then start
./mcp-host.sh status             # Show host, MCP connection state, and reranker health
./mcp-host.sh logs               # Tail host log (last 50 lines, follow mode)
./mcp-host.sh logs 100           # Tail last 100 lines of host log
./mcp-host.sh logs reranker      # Tail reranker log
./mcp-host.sh logs reranker 100  # Tail last 100 lines of reranker log
```

**Environment overrides:**

```bash
GO_BIN=/usr/local/go/bin/go CONFIG=/path/to/config.yaml ./mcp-host.sh start
RERANKER_SCRIPT=/path/to/reranker.py RERANKER_PORT=8090 ./mcp-host.sh start
```

---

## Project Structure

```
.
├── cmd/
│   └── main.go                  # Entry point
├── internal/
│   ├── chat/
│   │   └── handler.go           # Tool-calling loop, Ollama orchestration
│   ├── config/
│   │   └── config.go            # YAML config loader
│   ├── mcp/
│   │   ├── client.go            # MCP client (tool discovery + invocation)
│   │   ├── protocol.go          # MCP JSON-RPC types
│   │   ├── stdio.go             # Stdio transport (subprocess)
│   │   ├── http.go              # HTTP transport
│   │   └── transport.go         # Transport interface
│   ├── ollama/
│   │   └── client.go            # Ollama REST client + tool conversion
│   └── server/
│       └── http.go              # HTTP + WebSocket server
├── web/
│   └── index.html               # Single-page chat UI
├── config.yaml.example          # Configuration template (copy to config.yaml)
├── mcp-host.sh                  # Start/stop/status script (manages host + reranker)
├── go.mod
└── go.sum
```

---

## How It Works

1. On startup the host connects to each configured MCP server and discovers its tools.
2. When a user sends a message the host forwards it to Ollama along with the tool definitions.
3. If Ollama requests a tool call, the host executes it on the appropriate MCP server and feeds the result back.
4. Steps 2–3 repeat (up to 5 times) until Ollama returns a plain text response.
5. If the model returns empty content (common with small models on large tool results), the raw tool output is formatted and shown directly.

---

## Recommended Models

Tool calling works best with larger, instruction-tuned models. Tested with:

| Model | Notes |
|---|---|
| `minimax-m2.5:cloud` | Recommended — strong tool use, cloud-hosted via Ollama |

Small models (`< 1B params`) often return empty responses after receiving large tool results.

---

## MCP Server: Pinecone RAG

For document-grounded Q&A, pair this host with the companion [mcp-pinecone-rag](https://github.com/aravindanugonda/mcp-pinecone-rag) server. It exposes a `rag_query` tool backed by a **three-layer retrieval pipeline**:

1. **Semantic search** — query embedded via Google Vertex AI `text-embedding-005`, top-20 candidates retrieved from Pinecone
2. **Keyword boost** — exact matches for domain tokens (module names, error codes, ALL-CAPS acronyms) promoted in the ranking
3. **Cross-encoder reranker** — optional local Python service (`reranker.py`) reorders the candidates by full contextual relevance using `BAAI/bge-reranker-base` via [fastembed](https://github.com/qdrant/fastembed) (no PyTorch required)

### Reranker setup

The reranker is started and stopped automatically by this script. It runs as a local Flask server on port `8090`.

**First-time setup** — the script creates a Python venv and installs `fastembed` + `flask` automatically:

```bash
# Debian/Ubuntu: ensure venv support is available
apt install python3-venv

./mcp-host.sh start   # venv + deps installed on first run, ~150 MB
```

**In `config.yaml`**, add `RERANKER_URL` to the `pinecone-rag` server env:

```yaml
  - name: "pinecone-rag"
    type: "stdio"
    command: "/path/to/mcp-pinecone-rag/mcp-pinecone-rag"
    args: []
    env:
      PINECONE_API_KEY: "..."
      GOOGLE_CLOUD_PROJECT: "..."
      GOOGLE_APPLICATION_CREDENTIALS: "/path/to/service-account.json"
      VERTEX_AI_LOCATION: "us-central1"
      PINECONE_INDEX_HOST: "..."
      RAG_TOP_K: "5"
      RERANKER_URL: "http://localhost:8090"
```

If `RERANKER_URL` is not set or the reranker service is unavailable, the RAG server falls back gracefully to the hybrid keyword-boosted results.

See [mcp-pinecone-rag](https://github.com/aravindanugonda/mcp-pinecone-rag) for full setup instructions.

---

## License

MIT
