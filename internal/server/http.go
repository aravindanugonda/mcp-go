package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"

	"mcp-host/internal/chat"
)

// Server represents the HTTP/WebSocket server
type Server struct {
	addr       string
	handler    *chat.Handler
	httpServer *http.Server
	upgrader   websocket.Upgrader
	clients    map[*websocket.Conn]bool
	clientsMu  sync.RWMutex
}

// NewServer creates a new server
func NewServer(addr string, handler *chat.Handler) *Server {
	return &Server{
		addr:    addr,
		handler: handler,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 65536,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins in development
			},
		},
		clients: make(map[*websocket.Conn]bool),
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/chat", s.handleChat)
	mux.HandleFunc("/api/tools", s.handleTools)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.Handle("/", http.FileServer(http.Dir("./web")))

	s.httpServer = &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	log.Printf("Server starting on %s", s.addr)
	return s.httpServer.ListenAndServe()
}

// ChatRequest represents a chat API request
type ChatRequest struct {
	Message string `json:"message"`
}

// ChatResponse represents a chat API response
type ChatResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

// handleChat handles chat API requests
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Process chat message
	response, err := s.handler.Chat(req.Message)
	if err != nil {
		resp := ChatResponse{
			Error: err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	resp := ChatResponse{
		Response: response,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleTools handles tools API requests
func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tools, err := s.handler.ListTools()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(tools))
}

// StatusResponse represents a status API response
type StatusResponse struct {
	MCP map[string]bool `json:"mcp"`
}

// handleStatus handles status API requests
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.handler.GetMCPStatus()

	resp := StatusResponse{
		MCP: status,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleWebSocket handles WebSocket connections for real-time chat
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// Register client
	s.clientsMu.Lock()
	s.clients[conn] = true
	s.clientsMu.Unlock()

	log.Printf("WebSocket client connected")

	// Handle messages
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			break
		}

		// Parse message
		var req ChatRequest
		if err := json.Unmarshal(message, &req); err != nil {
			errorResp, _ := json.Marshal(ChatResponse{Error: "Invalid message format"})
			conn.WriteMessage(websocket.TextMessage, errorResp)
			continue
		}

		// Process chat
		response, err := s.handler.Chat(req.Message)
		var resp ChatResponse
		if err != nil {
			resp = ChatResponse{Error: err.Error()}
		} else {
			resp = ChatResponse{Response: response}
		}

		respData, _ := json.Marshal(resp)
		if err := conn.WriteMessage(websocket.TextMessage, respData); err != nil {
			log.Printf("WebSocket write error: %v", err)
			break
		}
	}

	// Unregister client
	s.clientsMu.Lock()
	delete(s.clients, conn)
	s.clientsMu.Unlock()

	conn.Close()
	log.Printf("WebSocket client disconnected")
}

// Broadcast sends a message to all connected WebSocket clients
func (s *Server) Broadcast(message []byte) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for client := range s.clients {
		if err := client.WriteMessage(websocket.TextMessage, message); err != nil {
			log.Printf("Broadcast error: %v", err)
			client.Close()
		}
	}
}

// Close closes all WebSocket connections and shuts down the HTTP server
func (s *Server) Close() {
	s.clientsMu.Lock()
	for client := range s.clients {
		client.Close()
	}
	s.clientsMu.Unlock()

	if s.httpServer != nil {
		s.httpServer.Close()
	}
}