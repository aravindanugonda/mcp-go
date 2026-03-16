package mcp

import (
	"encoding/json"
)

// JSON-RPC 2.0 Types

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}    `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// JSONRPCError codes
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32000
)

// Initialize Request/Response

// InitializeParams represents the parameters for the initialize method
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo     `json:"clientInfo"`
}

// ClientCapabilities represents client capabilities
type ClientCapabilities struct {
	Tools        *struct{}      `json:"tools,omitempty"`
	Resources    *struct{}      `json:"resources,omitempty"`
	Prompts      *struct{}      `json:"prompts,omitempty"`
	Sampling     *SamplingCapability `json:"sampling,omitempty"`
}

// SamplingCapability represents sampling capability
type SamplingCapability struct {
	// Empty struct for basic sampling support
}

// ClientInfo represents client information
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult represents the result of the initialize method
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo     `json:"serverInfo"`
}

// ServerCapabilities represents server capabilities
type ServerCapabilities struct {
	Tools       *ToolsCapability       `json:"tools,omitempty"`
	Resources   *ResourcesCapability   `json:"resources,omitempty"`
	Prompts     *PromptsCapability     `json:"prompts,omitempty"`
	Sampling    *ServerSamplingCapability `json:"sampling,omitempty"`
}

// ToolsCapability represents tools capability
type ToolsCapability struct{}

// ResourcesCapability represents resources capability
type ResourcesCapability struct{}

// PromptsCapability represents prompts capability
type PromptsCapability struct{}

// ServerSamplingCapability represents server sampling capability
type ServerSamplingCapability struct{}

// ServerInfo represents server information
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tools List Request/Response

// ToolsListParams represents the parameters for tools/list
type ToolsListParams struct{}

// ToolDefinition represents a tool definition
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolsListResult represents the result of tools/list
type ToolsListResult struct {
	Tools []ToolDefinition `json:"tools"`
}

// Tools Call Request/Response

// ToolsCallParams represents the parameters for tools/call
type ToolsCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// ToolCallResult represents the result of a tool call
type ToolCallResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ToolsCallResult represents the result of tools/call
type ToolsCallResult struct {
	Content []ContentBlock `json:"content"`
}

// ContentBlock represents a content block
type ContentBlock struct {
	Type     string `json:"type"` // "text", "image", "audio", "toolUse", "toolResult"
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`

	// Tool use fields
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`

	// Tool result fields
	ToolUseID string `json:"toolUseId,omitempty"`
}

// Notification: Initialized

// InitializedNotification represents the initialized notification
type InitializedNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
}

// Ping Request/Response

// PingParams represents the parameters for ping
type PingParams struct{}

// PingResult represents the result of ping
type PingResult struct{}

// Request ID generator
type IDGenerator struct {
	id int
}

func NewIDGenerator() *IDGenerator {
	return &IDGenerator{id: 0}
}

func (g *IDGenerator) Next() interface{} {
	g.id++
	return g.id
}