package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"context"
)

// HTTPTransport implements MCP transport over HTTP/SSE
type HTTPTransport struct {
	url    string
	client *http.Client
}

// NewHTTPTransport creates a new HTTP transport
func NewHTTPTransport(url string) *HTTPTransport {
	return &HTTPTransport{
		url:    url,
		client: &http.Client{},
	}
}

// Connect establishes the HTTP connection (simple connectivity check)
func (t *HTTPTransport) Connect() error {
	// Send a ping to verify connectivity
	req, err := http.NewRequest("POST", t.url, bytes.NewBuffer([]byte(`{"jsonrpc":"2.0","method":"ping","id":0}`)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to MCP server: %w", err)
	}
	defer resp.Body.Close()

	return nil
}

// Close closes the HTTP connection (no-op for HTTP)
func (t *HTTPTransport) Close() error {
	t.client.CloseIdleConnections()
	return nil
}

// Send sends a JSON-RPC request and returns the response
func (t *HTTPTransport) Send(req *JSONRPCRequest) (*JSONRPCResponse, error) {
	// Marshal request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequest("POST", t.url, bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	httpResp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer httpResp.Body.Close()

	// Read response body
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse JSON-RPC response
	var resp JSONRPCResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp, nil
}

// SendNotification sends a JSON-RPC notification
func (t *HTTPTransport) SendNotification(method string, params interface{}) error {
	notif := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
	}

	if params != nil {
		paramsData, err := json.Marshal(params)
		if err != nil {
			return err
		}
		notif.Params = paramsData
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(
		context.Background(),
		"POST",
		t.url,
		bytes.NewBuffer(data),
	)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// For notifications, we don't wait for a response
	// But HTTP will still return a response, so we consume it
	io.Copy(io.Discard, resp.Body)

	return nil
}