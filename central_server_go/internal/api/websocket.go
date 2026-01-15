package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// WebSocketHub manages WebSocket connections
type WebSocketHub struct {
	clients    map[*WebSocketClient]bool
	broadcast  chan []byte
	register   chan *WebSocketClient
	unregister chan *WebSocketClient
	mu         sync.RWMutex

	// Cached broadcast data (single JSON encode for all clients)
	cachedData   []byte
	cachedDataMu sync.RWMutex
}

// WebSocketClient represents a WebSocket connection
type WebSocketClient struct {
	hub  *WebSocketHub
	conn *websocket.Conn
	send chan []byte
}

// NewWebSocketHub creates a new WebSocket hub
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[*WebSocketClient]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *WebSocketClient),
		unregister: make(chan *WebSocketClient),
	}
}

// Run starts the hub main loop
func (h *WebSocketHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("WebSocket client connected. Total: %d", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("WebSocket client disconnected. Total: %d", len(h.clients))

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends a message to all connected clients
func (h *WebSocketHub) Broadcast(message interface{}) {
	data, err := json.Marshal(message)
	if err != nil {
		log.Printf("Failed to marshal WebSocket message: %v", err)
		return
	}

	select {
	case h.broadcast <- data:
	default:
		log.Println("Broadcast channel full, message dropped")
	}
}

// ClientCount returns the number of connected clients
func (h *WebSocketHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// HandleWebSocket handles WebSocket connections
func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade WebSocket: %v", err)
		return
	}

	client := &WebSocketClient{
		hub:  s.wsHub,
		conn: conn,
		send: make(chan []byte, 256),
	}

	s.wsHub.register <- client

	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump()

	// Start fleet state broadcasting
	go s.broadcastFleetState(client)
}

// readPump reads messages from the WebSocket connection
func (c *WebSocketClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512 * 1024)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		// Handle incoming messages (e.g., subscriptions)
		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err == nil {
			// Process message if needed
			log.Printf("WebSocket message received: %v", msg)
		}
	}
}

// writePump writes messages to the WebSocket connection
func (c *WebSocketClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current WebSocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// StartBroadcastLoop starts a single goroutine that broadcasts to all clients
// This is more efficient than per-client goroutines as it encodes JSON only once
func (s *Server) StartBroadcastLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.wsHub.mu.RLock()
		clientCount := len(s.wsHub.clients)
		s.wsHub.mu.RUnlock()

		if clientCount == 0 {
			continue
		}

		// Build and encode data once for all clients
		data := s.buildFleetStateJSON()
		if data == nil {
			continue
		}

		// Broadcast to all clients
		s.wsHub.broadcast <- data
	}
}

// buildFleetStateJSON builds fleet state and returns encoded JSON
// Uses pre-allocated structures to minimize allocations
func (s *Server) buildFleetStateJSON() []byte {
	snapshot := s.stateManager.GetSnapshot()
	now := time.Now()

	// Pre-allocate with expected capacity
	robots := make([]RobotStateWS, 0, len(snapshot.Robots))
	for _, robot := range snapshot.Robots {
		// Determine execution phase
		executionPhase := "idle"
		if !robot.IsOnline {
			executionPhase = "offline"
		} else if robot.IsExecuting {
			if robot.CurrentStepID == "" {
				executionPhase = "starting"
			} else {
				executionPhase = "executing"
			}
		}

		r := RobotStateWS{
			ID:             robot.ID,
			Name:           robot.Name,
			State:          robot.CurrentState,
			StateCode:      robot.CurrentStateCode,
			CurrentGraphID: robot.CurrentGraphID,
			ExecutionPhase: executionPhase,
			SemanticTags:   robot.SemanticTags,
			IsOnline:       robot.IsOnline,
			IsExecuting:    robot.IsExecuting,
			StalenessSec:   now.Sub(robot.LastSeen).Seconds(),
		}
		if robot.IsExecuting && robot.CurrentTaskID != "" {
			r.CurrentTask = &TaskInfoWS{
				ID:          robot.CurrentTaskID,
				CurrentStep: robot.CurrentStepID,
			}
		}
		robots = append(robots, r)
	}

	// Get active tasks (with preloaded ActionGraph to avoid N+1)
	tasks := make([]TaskStateWS, 0)
	if dbTasks, err := s.repo.GetActiveTasks(); err == nil {
		for _, task := range dbTasks {
			t := TaskStateWS{
				ID:            task.ID,
				Status:        task.Status,
				CurrentStep:   task.CurrentStepIndex + 1,
			}
			if task.ActionGraphID.Valid {
				t.ActionGraphID = task.ActionGraphID.String
			}
			if task.AgentID.Valid {
				t.AgentID = task.AgentID.String
			}
			if task.CurrentStepID.Valid {
				t.CurrentStepID = task.CurrentStepID.String
			}
			if task.StartedAt.Valid {
				t.StartedAt = task.StartedAt.Time.Format(time.RFC3339)
			}
			// Step count from preloaded ActionGraph
			if task.ActionGraph != nil {
				var steps []interface{}
				json.Unmarshal(task.ActionGraph.Steps, &steps)
				t.TotalSteps = len(steps)
			}
			tasks = append(tasks, t)
		}
	}

	message := FleetStateWS{
		Timestamp: now.Format(time.RFC3339),
		Robots:    robots,
		Tasks:     tasks,
	}

	data, err := json.Marshal(message)
	if err != nil {
		log.Printf("Failed to marshal fleet state: %v", err)
		return nil
	}
	return data
}

// Optimized WebSocket response structures (avoid map[string]interface{})
type FleetStateWS struct {
	Timestamp string        `json:"timestamp"`
	Robots    []RobotStateWS `json:"robots"`
	Tasks     []TaskStateWS  `json:"tasks"`
}

type RobotStateWS struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	State          string       `json:"state"`
	StateCode      string       `json:"state_code,omitempty"`       // Enhanced state code (e.g., "pick:executing")
	CurrentGraphID string       `json:"current_graph_id,omitempty"` // Currently executing graph ID
	ExecutionPhase string       `json:"execution_phase"`            // Explicit phase: idle, starting, executing, completing
	SemanticTags   []string     `json:"semantic_tags,omitempty"`    // State semantic tags
	IsOnline       bool         `json:"is_online"`
	IsExecuting    bool         `json:"is_executing"`               // Explicit execution flag
	StalenessSec   float64      `json:"staleness_sec"`
	CurrentTask    *TaskInfoWS  `json:"current_task,omitempty"`
}

type TaskInfoWS struct {
	ID          string `json:"id"`
	CurrentStep string `json:"current_step,omitempty"`
}

type TaskStateWS struct {
	ID            string `json:"id"`
	ActionGraphID string `json:"action_graph_id,omitempty"`
	AgentID       string `json:"agent_id,omitempty"`
	Status        string `json:"status"`
	CurrentStepID string `json:"current_step_id,omitempty"`
	CurrentStep   int    `json:"current_step"`
	TotalSteps    int    `json:"total_steps"`
	StartedAt     string `json:"started_at,omitempty"`
}

// broadcastFleetState - legacy per-client broadcast (kept for backward compatibility)
// New clients should use the centralized broadcast loop
func (s *Server) broadcastFleetState(client *WebSocketClient) {
	// Simply wait for disconnect - broadcast is handled by StartBroadcastLoop
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.wsHub.mu.RLock()
			_, ok := s.wsHub.clients[client]
			s.wsHub.mu.RUnlock()
			if !ok {
				return
			}
		}
	}
}

// FleetStateMessage represents a fleet state update message
type FleetStateMessage struct {
	Type      string                 `json:"type"`
	Timestamp int64                  `json:"timestamp"`
	Robots    map[string]interface{} `json:"robots"`
}

// TaskUpdateMessage represents a task update message
type TaskUpdateMessage struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"`
	TaskID    string `json:"task_id"`
	AgentID   string `json:"agent_id"`
	Status    string `json:"status"`
	StepID    string `json:"step_id,omitempty"`
	Message   string `json:"message,omitempty"`
}

// BroadcastTaskUpdate sends a task update to all clients
func (s *Server) BroadcastTaskUpdate(taskID, agentID, status, stepID, message string) {
	msg := TaskUpdateMessage{
		Type:      "task_update",
		Timestamp: time.Now().UnixMilli(),
		TaskID:    taskID,
		AgentID:   agentID,
		Status:    status,
		StepID:    stepID,
		Message:   message,
	}
	s.wsHub.Broadcast(msg)
}

// AgentUpdateMessage represents an agent status update message
type AgentUpdateMessage struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"`
	AgentID   string `json:"agent_id"`
	Status    string `json:"status"`
}

// BroadcastAgentUpdate sends an agent update to all clients
func (h *WebSocketHub) BroadcastAgentUpdate(agentID string, status string) {
	msg := AgentUpdateMessage{
		Type:      "agent_update",
		Timestamp: time.Now().UnixMilli(),
		AgentID:   agentID,
		Status:    status,
	}
	h.Broadcast(msg)
}

// CapabilityUpdateMessage represents a capability update message
type CapabilityUpdateMessage struct {
	Type         string      `json:"type"`
	Timestamp    int64       `json:"timestamp"`
	AgentID      string      `json:"agent_id"`
	Capabilities interface{} `json:"capabilities"`
}

// BroadcastCapabilityUpdate sends a capability update to all clients
func (h *WebSocketHub) BroadcastCapabilityUpdate(agentID string, capabilities interface{}) {
	msg := CapabilityUpdateMessage{
		Type:         "capability_update",
		Timestamp:    time.Now().UnixMilli(),
		AgentID:      agentID,
		Capabilities: capabilities,
	}
	h.Broadcast(msg)
}
