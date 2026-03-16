package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"mcp-host/internal/chat"
	"mcp-host/internal/config"
	"mcp-host/internal/mcp"
	"mcp-host/internal/ollama"
	"mcp-host/internal/server"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
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