package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mcp-host/internal/chat"
	"mcp-host/internal/config"
	"mcp-host/internal/mcp"
	"mcp-host/internal/ollama"
	"mcp-host/internal/server"
)

// registryServer mirrors the transport shape returned by mcp-registry.
type registryTransport struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	URL     string            `json:"url"`
}

type registryEntry struct {
	Name      string            `json:"name"`
	Transport registryTransport `json:"transport"`
}

type registryResponse struct {
	Servers []registryEntry `json:"servers"`
}

// fetchFromRegistry queries mcp-registry and returns server configs not already
// present in localServers (local config takes precedence).
func fetchFromRegistry(url string, localServers []config.MCPServer) []config.MCPServer {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url + "/v0.1/servers")
	if err != nil {
		log.Printf("Registry unreachable at %s: %v", url, err)
		return nil
	}
	defer resp.Body.Close()

	var reg registryResponse
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		log.Printf("Registry response parse error: %v", err)
		return nil
	}

	local := make(map[string]bool, len(localServers))
	for _, s := range localServers {
		local[s.Name] = true
	}

	var extra []config.MCPServer
	for _, e := range reg.Servers {
		if local[e.Name] {
			continue // local config wins
		}
		s := config.MCPServer{
			Name:    e.Name,
			Type:    e.Transport.Type,
			Command: e.Transport.Command,
			Args:    e.Transport.Args,
			Env:     e.Transport.Env,
			URL:     e.Transport.URL,
		}
		extra = append(extra, s)
		log.Printf("  [registry] discovered server: %s (%s)", s.Name, s.Type)
	}
	return extra
}

func main() {
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Merge servers from registry (if configured)
	if cfg.RegistryURL != "" {
		log.Printf("Fetching servers from registry: %s", cfg.RegistryURL)
		extra := fetchFromRegistry(cfg.RegistryURL, cfg.MCPServers)
		cfg.MCPServers = append(cfg.MCPServers, extra...)
	}

	// Create Ollama client
	ollamaClient := ollama.NewClient(cfg.Ollama.BaseURL, cfg.Ollama.Model)
	log.Printf("Connected to Ollama at %s with model %s", cfg.Ollama.BaseURL, cfg.Ollama.Model)

	// Create chat handler
	chatHandler := chat.NewHandler(ollamaClient)

	// Connect to MCP servers
	for _, serverConfig := range cfg.MCPServers {
		log.Printf("Connecting to MCP server: %s (%s)", serverConfig.Name, serverConfig.Type)

		var transport mcp.Transport

		switch serverConfig.Type {
		case "stdio":
			transport = mcp.NewStdioTransport(serverConfig.Command, serverConfig.Args, serverConfig.Env)
		case "http":
			transport = mcp.NewHTTPTransport(serverConfig.URL)
		case "sse":
			transport = mcp.NewSSETransport(serverConfig.URL)
		default:
			log.Printf("Unknown MCP server type: %s", serverConfig.Type)
			continue
		}

		client := mcp.NewClient(serverConfig.Name, transport)

		if err := client.Connect(); err != nil {
			log.Printf("Failed to connect to MCP server %s: %v", serverConfig.Name, err)
			continue
		}

		log.Printf("Connected to MCP server: %s", serverConfig.Name)
		log.Printf("  Server info: %s v%s", client.GetServerInfo().Name, client.GetServerInfo().Version)

		tools := client.GetTools()
		log.Printf("  Available tools: %d", len(tools))
		for _, tool := range tools {
			log.Printf("    - %s: %s", tool.Name, tool.Description)
		}

		chatHandler.AddMCPClient(serverConfig.Name, client)
	}

	// Create and start HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpServer := server.NewServer(addr, chatHandler)

	// Handle shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down...")
		httpServer.Close()
		os.Exit(0)
	}()

	// Start server
	log.Printf("Starting MCP Host on %s", addr)
	log.Printf("Open http://%s in your browser to use the chat interface", addr)
	log.Printf("API endpoints:")
	log.Printf("  POST /api/chat - Send a chat message")
	log.Printf("  GET  /api/tools - List available tools")
	log.Printf("  GET  /api/status - Check MCP server status")
	log.Printf("  WS  /ws - WebSocket chat")

	if err := httpServer.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}