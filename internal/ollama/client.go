package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"mcp-host/internal/mcp"
)


// Client represents an Ollama API client
type Client struct {
	baseURL string
	model   string
	client  *http.Client
}

// Message represents a chat message
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Tool represents a tool definition for the LLM
type Tool struct {
	Type     string          `json:"type"`
	Function ToolFunction    `json:"function"`
}

// ToolFunction represents the function details
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ChatRequest represents a chat completion request
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Tools    []Tool    `json:"tools,omitempty"`
}

// ChatResponse represents a chat completion response
type ChatResponse struct {
	Model     string        `json:"model"`
	Message   ResponseMessage `json:"message"`
	Done      bool          `json:"done"`
	StopReason string       `json:"stop_reason,omitempty"`
}

// ResponseMessage represents a response message
type ResponseMessage struct {
	Role    string         `json:"role"`
	Content string         `json:"content,omitempty"`
	ToolCalls []ToolCall   `json:"tool_calls,omitempty"`
}

// ToolCall represents a tool call from the LLM
type ToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function ToolCallFunction       `json:"function"`
}

// ToolCallFunction represents the function to call
type ToolCallFunction struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// NewClient creates a new Ollama client
func NewClient(baseURL, model string) *Client {
	return &Client{
		baseURL: baseURL,
		model:   model,
		client: &http.Client{
			Timeout: 300 * time.Second,
		},
	}
}

// Chat sends a chat completion request and returns the response
func (c *Client) Chat(messages []Message) (*ChatResponse, error) {
	req := ChatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   false,
	}

	return c.chat(req)
}

// ChatWithTools sends a chat completion request with tools and returns the response
func (c *Client) ChatWithTools(messages []Message, tools []Tool) (*ChatResponse, error) {
	req := ChatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   false,
		Tools:    tools,
	}

	return c.chat(req)
}

func (c *Client) chat(req ChatRequest) (*ChatResponse, error) {
	url := fmt.Sprintf("%s/api/chat", c.baseURL)

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama error (status %d): %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &chatResp, nil
}

// ConvertMCPTools converts MCP tool definitions to Ollama format
func ConvertMCPTools(mcpTools []mcp.ToolDefinition) []Tool {
	tools := make([]Tool, 0, len(mcpTools))

	for _, mcpTool := range mcpTools {
		tools = append(tools, Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        mcpTool.Name,
				Description: mcpTool.Description,
				Parameters:  mcpTool.InputSchema,
			},
		})
	}

	return tools
}

// ConvertToolCallToMessage builds the assistant + tool-result messages for the next iteration.
// Ollama expects: an assistant message carrying tool_calls, followed by one "tool" role message
// per result (matched by position).
func ConvertToolCallToMessage(toolCalls []ToolCall, toolResults []string) []Message {
	messages := make([]Message, 0, 1+len(toolCalls))

	// Assistant turn: carry the tool_calls field (no content)
	messages = append(messages, Message{
		Role:      "assistant",
		ToolCalls: toolCalls,
	})

	// One "tool" message per result, in the same order as the calls
	for i := range toolCalls {
		result := "Tool result not found"
		if i < len(toolResults) {
			result = toolResults[i]
		}
		messages = append(messages, Message{
			Role:    "tool",
			Content: result,
		})
	}

	return messages
}