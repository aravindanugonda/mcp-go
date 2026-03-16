package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// StdioTransport implements MCP transport over stdio
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.Writer
	stdout *bufio.Reader
	stderr *bufio.Reader

	mu      sync.Mutex
	pending map[string]chan *JSONRPCResponse
	closed  bool
}

// NewStdioTransport creates a new stdio transport
func NewStdioTransport(command string, args []string, envVars map[string]string) *StdioTransport {
	cmd := exec.Command(command, args...)

	// Apply environment variables on top of the current process environment
	env := os.Environ()
	for k, v := range envVars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	return &StdioTransport{
		cmd:     cmd,
		pending: make(map[string]chan *JSONRPCResponse),
	}
}

// Connect establishes the stdio connection
func (t *StdioTransport) Connect() error {
	stdinPipe, err := t.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	t.stdin = stdinPipe

	stdout, err := t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	t.stdout = bufio.NewReader(stdout)

	stderr, err := t.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	t.stderr = bufio.NewReader(stderr)

	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Start a single background goroutine for reading — no goroutine-per-poll
	go t.readLoop()

	// Give the process a moment to initialize
	time.Sleep(100 * time.Millisecond)

	return nil
}

// Close closes the stdio connection
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	// Fail all pending requests immediately so callers don't block
	for key, ch := range t.pending {
		ch <- &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      key,
			Error:   &JSONRPCError{Code: InternalError, Message: "transport closed"},
		}
		delete(t.pending, key)
	}

	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
		t.cmd.Wait()
	}
	return nil
}

// Send sends a JSON-RPC request and waits for response
func (t *StdioTransport) Send(req *JSONRPCRequest) (*JSONRPCResponse, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport is closed")
	}

	// Use a string key so int IDs and float64-unmarshaled IDs both map to the same key
	key := fmt.Sprintf("%v", req.ID)
	respChan := make(chan *JSONRPCResponse, 1)
	t.pending[key] = respChan
	t.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		t.mu.Lock()
		delete(t.pending, key)
		t.mu.Unlock()
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Write JSON followed by newline (JSON-LDF)
	if _, err = t.stdin.Write(append(data, '\n')); err != nil {
		t.mu.Lock()
		delete(t.pending, key)
		t.mu.Unlock()
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	select {
	case resp := <-respChan:
		t.mu.Lock()
		delete(t.pending, key)
		t.mu.Unlock()
		return resp, nil
	case <-time.After(30 * time.Second):
		t.mu.Lock()
		delete(t.pending, key)
		t.mu.Unlock()
		return nil, fmt.Errorf("request timeout")
	}
}

// SendNotification sends a JSON-RPC notification (no response expected)
func (t *StdioTransport) SendNotification(method string, params interface{}) error {
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

	_, err = t.stdin.Write(append(data, '\n'))
	return err
}

// readLoop reads responses from stdout in a single goroutine (no per-poll goroutine leak)
func (t *StdioTransport) readLoop() {
	for {
		line, err := t.stdout.ReadString('\n')
		if err != nil {
			// EOF or pipe closed — process exited, exit cleanly
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			// Skip malformed lines (e.g. startup log noise)
			continue
		}

		key := fmt.Sprintf("%v", resp.ID)
		t.mu.Lock()
		if ch, ok := t.pending[key]; ok {
			ch <- &resp
		}
		t.mu.Unlock()
	}
}

// StderrReader returns a reader for stderr (useful for debugging)
func (t *StdioTransport) StderrReader() *bufio.Reader {
	return t.stderr
}
