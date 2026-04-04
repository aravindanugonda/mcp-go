package chat

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"mcp-host/internal/mcp"
	"mcp-host/internal/ollama"
)

// Handler handles chat interactions with MCP tools
type Handler struct {
	ollamaClient *ollama.Client
	mcpClients   map[string]*mcp.Client
	mu           sync.RWMutex

	// Configuration
	maxToolIterations int
}

// NewHandler creates a new chat handler
func NewHandler(ollamaClient *ollama.Client) *Handler {
	return &Handler{
		ollamaClient:      ollamaClient,
		mcpClients:        make(map[string]*mcp.Client),
		maxToolIterations: 5,
	}
}

// AddMCPClient adds an MCP client to the handler
func (h *Handler) AddMCPClient(name string, client *mcp.Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.mcpClients[name] = client
}

// GetAllTools returns all tools from all MCP servers
func (h *Handler) GetAllTools() []mcp.ToolDefinition {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var allTools []mcp.ToolDefinition
	for _, client := range h.mcpClients {
		tools := client.GetTools()
		allTools = append(allTools, tools...)
	}

	return allTools
}

const systemPrompt = `You are a helpful AI assistant with access to tools from connected MCP servers.
Guidelines:
- When answering questions about documentation, call rag_query once (twice at most for distinct sub-topics), then write a complete answer from the retrieved context. Do not keep re-querying for the same topic.
- Always synthesize retrieved context into a clear, formatted answer. Never output raw tool results.
- Always wrap code or configuration in fenced markdown code blocks with the language name (e.g. ` + "```jcl" + `, ` + "```cobol" + `).
- When showing multiple files, use a separate code block per file preceded by its filename as a heading.
- Be concise. Do not repeat retrieved content verbatim — extract and explain what is relevant.
- If a tool call fails, explain the error clearly and suggest next steps.`

// Chat handles a chat message and returns the response
func (h *Handler) Chat(message string) (string, error) {
	// Build initial messages
	messages := []ollama.Message{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: message,
		},
	}

	// Get all available tools from MCP servers
	allTools := h.GetAllTools()

	// If no tools available, just do a simple chat
	if len(allTools) == 0 {
		resp, err := h.ollamaClient.Chat(messages)
		if err != nil {
			return "", err
		}
		return resp.Message.Content, nil
	}

	// Convert MCP tools to Ollama format
	ollamaTools := ollama.ConvertMCPTools(allTools)

	// Initial chat with tools
	resp, err := h.ollamaClient.ChatWithTools(messages, ollamaTools)
	if err != nil {
		return "", err
	}

	// Handle tool calls in a loop
	return h.handleToolCalls(resp, messages)
}

// handleToolCalls handles tool calls from the LLM
func (h *Handler) handleToolCalls(initialResp *ollama.ChatResponse, messages []ollama.Message) (string, error) {
	currentResp := initialResp

	var lastToolCalls []ollama.ToolCall
	var lastToolResults []string

	for iteration := 0; len(currentResp.Message.ToolCalls) > 0 && iteration < h.maxToolIterations; iteration++ {
		toolCalls := currentResp.Message.ToolCalls
		lastToolCalls = toolCalls

		// Execute tool calls in order and collect results as a slice (position-matched)
		toolResults := make([]string, len(toolCalls))
		for i, tc := range toolCalls {
			result, err := h.executeToolCall(tc)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}
			log.Printf("Tool %s: %d chars returned", tc.Function.Name, len(result))
			toolResults[i] = result
		}
		lastToolResults = toolResults

		// Append assistant-with-tool-calls + individual tool result messages
		messages = append(messages, ollama.ConvertToolCallToMessage(toolCalls, toolResults)...)

		// Re-fetch tools in case the server's tool list changed
		ollamaTools := ollama.ConvertMCPTools(h.GetAllTools())

		var err error
		currentResp, err = h.ollamaClient.ChatWithTools(messages, ollamaTools)
		if err != nil {
			// Model failed to process tool results (e.g. context overflow).
			// Present the raw tool results directly so the user sees something.
			log.Printf("Ollama error after tool results: %v — returning raw results", err)
			return buildFallbackResponse(lastToolCalls, lastToolResults), nil
		}
		log.Printf("Model reply after tools: content=%d chars, tool_calls=%d", len(currentResp.Message.Content), len(currentResp.Message.ToolCalls))
	}

	if currentResp.Message.Content != "" {
		return currentResp.Message.Content, nil
	}

	// Model returned empty content (common with small models on large tool results).
	// Show the tool results directly rather than a useless placeholder.
	if len(lastToolResults) > 0 {
		log.Printf("Model returned empty content — falling back to raw tool results")
		return buildFallbackResponse(lastToolCalls, lastToolResults), nil
	}

	return "Done.", nil
}

// buildFallbackResponse formats raw tool results as markdown when the model
// returns empty content after tool execution.
func buildFallbackResponse(toolCalls []ollama.ToolCall, toolResults []string) string {
	var sb strings.Builder
	for i, tc := range toolCalls {
		if i > 0 {
			sb.WriteString("\n\n---\n\n")
		}
		fmt.Fprintf(&sb, "**`%s`** result:\n\n", tc.Function.Name)
		sb.WriteString(toolResults[i])
	}
	return sb.String()
}

// executeToolCall executes a single tool call on the appropriate MCP server
func (h *Handler) executeToolCall(toolCall ollama.ToolCall) (string, error) {
	toolName := toolCall.Function.Name
	arguments := toolCall.Function.Arguments

	// Try each MCP client to find the tool
	h.mu.RLock()
	defer h.mu.RUnlock()

	for name, client := range h.mcpClients {
		tools := client.GetTools()
		for _, tool := range tools {
			if tool.Name == toolName {
				log.Printf("Executing tool %s on server %s", toolName, name)
				results, err := client.CallTool(toolName, arguments)
				if err != nil {
					return "", err
				}

				// Concatenate all content blocks (read_multiple_files returns one block per file)
				if len(results) > 0 {
					var parts []string
					for _, r := range results {
						if r.Text != "" {
							parts = append(parts, r.Text)
						}
					}
					combined := strings.Join(parts, "\n")
					// Truncate very large results to avoid context-length issues
					const maxResultLen = 12000
					if len(combined) > maxResultLen {
						combined = combined[:maxResultLen] + fmt.Sprintf("\n\n[... truncated — %d chars total ...]", len(combined))
					}
					return combined, nil
				}
				return "Tool executed successfully", nil
			}
		}
	}

	return "", fmt.Errorf("tool %s not found on any MCP server", toolName)
}

// GetMCPStatus returns the status of all MCP connections
func (h *Handler) GetMCPStatus() map[string]bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	status := make(map[string]bool)
	for name, client := range h.mcpClients {
		status[name] = client.IsInitialized()
	}

	return status
}

// ListTools returns a JSON representation of all available tools
func (h *Handler) ListTools() (string, error) {
	tools := h.GetAllTools()
	data, err := json.MarshalIndent(tools, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}