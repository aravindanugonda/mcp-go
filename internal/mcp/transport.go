package mcp

import (
	"encoding/json"
)

// Transport defines the interface for MCP transport implementations
type Transport interface {
	// Connect establishes the transport connection
	Connect() error

	// Close closes the transport connection
	Close() error

	// Send sends a JSON-RPC request and returns the response
	Send(req *JSONRPCRequest) (*JSONRPCResponse, error)

	// SendNotification sends a JSON-RPC notification (no response expected)
	SendNotification(method string, params interface{}) error
}

// BaseTransport provides common functionality for transports
type BaseTransport struct {
	// Common transport functionality
}

func (b *BaseTransport) marshalJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (b *BaseTransport) unmarshalJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}