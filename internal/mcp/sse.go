package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// idKey converts any JSON-decoded ID to a stable string key.
// json.Unmarshal decodes numbers into float64 when the target is interface{},
// so int(1) and float64(1) must both map to "1".
func idKey(id interface{}) string {
	return fmt.Sprintf("%v", id)
}

// SSETransport implements MCP transport over the SSE protocol used by FastMCP.
//
// Protocol:
//   1. GET /sse  →  SSE stream; first event is "endpoint" with data="/messages/?session_id=..."
//   2. POST <base>/messages/?session_id=...  →  send JSON-RPC requests (202 response, no body)
//   3. Responses arrive as SSE "message" events on the open GET stream
type SSETransport struct {
	sseURL      string
	baseURL     string
	messagesURL string

	client     *http.Client
	sseBody    io.ReadCloser
	sseScanner *bufio.Scanner // shared scanner — avoids losing buffered bytes

	mu      sync.Mutex
	pending map[string]chan *JSONRPCResponse
	done    chan struct{}
}

func NewSSETransport(sseURL string) *SSETransport {
	u, _ := url.Parse(sseURL)
	base := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	return &SSETransport{
		sseURL:  sseURL,
		baseURL: base,
		client:  &http.Client{Timeout: 0}, // no timeout — SSE stream is long-lived
		pending: make(map[string]chan *JSONRPCResponse),
		done:    make(chan struct{}),
	}
}

func (t *SSETransport) Connect() error {
	req, err := http.NewRequest("GET", t.sseURL, nil)
	if err != nil {
		return fmt.Errorf("sse: create request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("sse: connect: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("sse: server returned %d", resp.StatusCode)
	}

	// Create one scanner for the lifetime of this connection so buffered
	// bytes are never lost between readEndpointEvent and readLoop.
	scanner := bufio.NewScanner(resp.Body)
	sessionPath, err := readEndpointEvent(scanner)
	if err != nil {
		resp.Body.Close()
		return fmt.Errorf("sse: read endpoint event: %w", err)
	}

	t.messagesURL = t.baseURL + sessionPath
	t.sseBody = resp.Body
	t.sseScanner = scanner

	go t.readLoop()
	return nil
}

// readEndpointEvent reads SSE lines until it finds:
//
//	event: endpoint
//	data: /messages/?session_id=...
func readEndpointEvent(scanner *bufio.Scanner) (string, error) {
	var eventType, dataVal string

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			eventType = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			dataVal = strings.TrimPrefix(line, "data: ")
		case line == "":
			if eventType == "endpoint" && dataVal != "" {
				return dataVal, nil
			}
			eventType, dataVal = "", ""
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("stream ended before endpoint event")
}

// readLoop processes incoming SSE events and routes JSON-RPC responses to
// waiting Send() callers.
func (t *SSETransport) readLoop() {
	defer close(t.done)

	var dataLines []string

	for t.sseScanner.Scan() {
		line := t.sseScanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		} else if line == "" && len(dataLines) > 0 {
			raw := strings.Join(dataLines, "\n")
			dataLines = nil

			var resp JSONRPCResponse
			if err := json.Unmarshal([]byte(raw), &resp); err != nil {
				continue
			}

			t.mu.Lock()
			key := idKey(resp.ID)
			ch, ok := t.pending[key]
			if ok {
				delete(t.pending, key)
			}
			t.mu.Unlock()

			if ok {
				ch <- &resp
			}
		}
	}
}

func (t *SSETransport) Send(req *JSONRPCRequest) (*JSONRPCResponse, error) {
	ch := make(chan *JSONRPCResponse, 1)
	key := idKey(req.ID)

	t.mu.Lock()
	t.pending[key] = ch
	t.mu.Unlock()

	if err := t.post(req); err != nil {
		t.mu.Lock()
		delete(t.pending, key)
		t.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(30 * time.Second):
		t.mu.Lock()
		delete(t.pending, key)
		t.mu.Unlock()
		return nil, fmt.Errorf("sse: timeout waiting for response to %v", req.ID)
	}
}

func (t *SSETransport) SendNotification(method string, params interface{}) error {
	notif := &JSONRPCRequest{JSONRPC: "2.0", Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		notif.Params = b
	}
	return t.post(notif)
}

func (t *SSETransport) post(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("sse: marshal: %w", err)
	}

	req, err := http.NewRequest("POST", t.messagesURL, bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("sse: create post: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	postClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := postClient.Do(req)
	if err != nil {
		return fmt.Errorf("sse: post: %w", err)
	}
	resp.Body.Close()
	return nil
}

func (t *SSETransport) Close() error {
	if t.sseBody != nil {
		t.sseBody.Close()
	}
	return nil
}
