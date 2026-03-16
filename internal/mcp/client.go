package mcp

import (
	"encoding/json"
	"fmt"
)

// Client represents an MCP client connection to a server
type Client struct {
	name         string
	transport    Transport
	serverInfo   *ServerInfo
	serverCaps   *ServerCapabilities
	tools        []ToolDefinition
	idGen        *IDGenerator
	initialized  bool
}

// NewClient creates a new MCP client
func NewClient(name string, transport Transport) *Client {
	return &Client{
		name:    name,
		transport: transport,
		idGen:   NewIDGenerator(),
	}
}

// Connect connects to the MCP server
func (c *Client) Connect() error {
	if err := c.transport.Connect(); err != nil {
		return err
	}

	// Initialize the connection
	if err := c.Initialize(); err != nil {
		c.transport.Close()
		return err
	}

	return nil
}

// Close closes the client connection
func (c *Client) Close() error {
	return c.transport.Close()
}

// Initialize performs the MCP handshake
func (c *Client) Initialize() error {
	// Build initialize params
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities: ClientCapabilities{
			Tools:    &struct{}{},
			Sampling: &SamplingCapability{},
		},
		ClientInfo: ClientInfo{
			Name:    c.name,
			Version: "1.0.0",
		},
	}

	paramsData, err := json.Marshal(params)
	if err != nil {
		return err
	}

	// Create request
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.idGen.Next(),
		Method:  "initialize",
		Params:  paramsData,
	}

	// Send request
	resp, err := c.transport.Send(&req)
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s (code: %d)", resp.Error.Message, resp.Error.Code)
	}

	// Parse result
	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("failed to parse initialize result: %w", err)
	}

	c.serverInfo = &result.ServerInfo
	c.serverCaps = &result.Capabilities
	c.initialized = true

	// Send initialized notification
	if err := c.transport.SendNotification("initialized", nil); err != nil {
		return fmt.Errorf("failed to send initialized notification: %w", err)
	}

	// Load tools
	if err := c.loadTools(); err != nil {
		return fmt.Errorf("failed to load tools: %w", err)
	}

	return nil
}

// loadTools loads available tools from the server
func (c *Client) loadTools() error {
	// Check if server supports tools
	if c.serverCaps.Tools == nil {
		c.tools = []ToolDefinition{}
		return nil
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.idGen.Next(),
		Method:  "tools/list",
	}

	resp, err := c.transport.Send(&req)
	if err != nil {
		return fmt.Errorf("tools/list failed: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("tools/list error: %s (code: %d)", resp.Error.Message, resp.Error.Code)
	}

	var result ToolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("failed to parse tools list result: %w", err)
	}

	c.tools = result.Tools
	return nil
}

// CallTool calls a tool on the server
func (c *Client) CallTool(name string, arguments map[string]interface{}) ([]ContentBlock, error) {
	params := ToolsCallParams{
		Name:      name,
		Arguments: arguments,
	}

	paramsData, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.idGen.Next(),
		Method:  "tools/call",
		Params:  paramsData,
	}

	resp, err := c.transport.Send(&req)
	if err != nil {
		return nil, fmt.Errorf("tools/call failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tools/call error: %s (code: %d)", resp.Error.Message, resp.Error.Code)
	}

	var result ToolsCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tools/call result: %w", err)
	}

	return result.Content, nil
}

// Ping sends a ping to the server
func (c *Client) Ping() error {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.idGen.Next(),
		Method:  "ping",
	}

	resp, err := c.transport.Send(&req)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return fmt.Errorf("ping error: %s (code: %d)", resp.Error.Message, resp.Error.Code)
	}

	return nil
}

// GetTools returns the available tools
func (c *Client) GetTools() []ToolDefinition {
	return c.tools
}

// GetServerInfo returns server information
func (c *Client) GetServerInfo() *ServerInfo {
	return c.serverInfo
}

// GetServerCapabilities returns server capabilities
func (c *Client) GetServerCapabilities() *ServerCapabilities {
	return c.serverCaps
}

// IsInitialized returns whether the client is initialized
func (c *Client) IsInitialized() bool {
	return c.initialized
}