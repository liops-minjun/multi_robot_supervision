package api

import (
	"net/http"
	"time"

	"central_server_go/internal/db"
	"central_server_go/internal/executor"
	fleetgrpc "central_server_go/internal/grpc"
	"central_server_go/internal/state"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// Server represents the HTTP API server
type Server struct {
	router          *chi.Mux
	repo            *db.Repository
	stateManager    *state.GlobalStateManager
	scheduler       *executor.Scheduler
	wsHub           *WebSocketHub
	quicHandler     *fleetgrpc.RawQUICHandler
	definitionsPath string
}

// NewServer creates a new API server
func NewServer(repo *db.Repository, stateManager *state.GlobalStateManager, scheduler *executor.Scheduler, quicHandler *fleetgrpc.RawQUICHandler, definitionsPath string) *Server {
	s := &Server{
		repo:            repo,
		stateManager:    stateManager,
		scheduler:       scheduler,
		wsHub:           NewWebSocketHub(),
		quicHandler:     quicHandler,
		definitionsPath: definitionsPath,
	}

	s.setupRouter()

	// Start WebSocket hub (handles client registration/unregistration)
	go s.wsHub.Run()

	// Start centralized broadcast loop (single goroutine for all clients)
	// This is more efficient than per-client goroutines
	go s.StartBroadcastLoop()

	return s
}

// setupRouter configures all routes
func (s *Server) setupRouter() {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok"}`))
	})

	// API routes
	r.Route("/api", func(r chi.Router) {
		// Robots (Zero-Config Architecture)
		r.Route("/robots", func(r chi.Router) {
			r.Get("/", s.ListRobots)
			r.Post("/", s.RegisterRobot)          // New: Zero-config registration
			r.Post("/connect", s.ConnectRobot)    // Legacy: Keep for backward compatibility
			r.Get("/{robotID}", s.GetRobot)
			r.Patch("/{robotID}", s.UpdateRobot)  // New: Update robot metadata
			r.Delete("/{robotID}", s.DeleteRobot)
			r.Get("/{robotID}/commands", s.GetCommands)
			r.Post("/{robotID}/commands/{commandID}/result", s.ReportCommandResult)

			// Capability endpoints
			r.Put("/{robotID}/capabilities", s.RegisterCapabilities)
			r.Get("/{robotID}/capabilities", s.GetRobotCapabilities)
			r.Patch("/{robotID}/capabilities/status", s.UpdateCapabilityStatus)
		})

		// Fleet-wide Capabilities
		r.Route("/capabilities", func(r chi.Router) {
			r.Get("/", s.ListAllCapabilities)
			r.Get("/action-types", s.GetAllActionTypesWithStats)
			r.Get("/*", s.GetCapabilitiesByActionType)
		})

		// Action Graphs
		r.Route("/action-graphs", func(r chi.Router) {
			r.Get("/", s.ListActionGraphs)
			r.Post("/", s.CreateActionGraph)
			r.Get("/{graphID}", s.GetActionGraph)
			r.Put("/{graphID}", s.UpdateActionGraph)
			r.Delete("/{graphID}", s.DeleteActionGraph)
			r.Post("/{graphID}/execute", s.ExecuteActionGraph)
			r.Post("/{graphID}/validate", s.ValidateActionGraph)

			// Canonical Graph endpoints (new graph-optimized format)
			r.Get("/{graphID}/canonical", s.GetCanonicalGraph)
			r.Post("/{graphID}/validate-canonical", s.ValidateCanonicalGraph)
			r.Post("/{graphID}/deploy/{agentID}", s.DeployActionGraphToAgent)
		})

		// Agents
		r.Route("/agents", func(r chi.Router) {
			r.Get("/", s.ListAgents)
			r.Post("/", s.CreateAgent)
			r.Get("/connection-status", s.GetAgentConnectionStatus) // Heartbeat monitoring for all agents
			r.Get("/{agentID}", s.GetAgent)
			r.Delete("/{agentID}", s.DeleteAgent)
			r.Get("/{agentID}/capabilities", s.GetAgentCapabilities)
			r.Get("/{agentID}/connection-status", s.GetSingleAgentConnectionStatus) // Heartbeat monitoring for single agent

			r.Route("/{agentID}/action-graphs", func(r chi.Router) {
				r.Get("/", s.ListAgentActionGraphs)
				r.Post("/", s.AssignActionGraph)
				r.Get("/{graphID}", s.GetAgentActionGraph)
				r.Delete("/{graphID}", s.RemoveAgentActionGraph)
				r.Post("/{graphID}/deploy", s.DeployActionGraph)
				r.Get("/{graphID}/logs", s.GetDeploymentLogs)
			})
		})

		// Tasks
		r.Route("/tasks", func(r chi.Router) {
			r.Get("/", s.ListTasks)
			r.Get("/{taskID}", s.GetTask)
			r.Post("/{taskID}/cancel", s.CancelTask)
			r.Post("/{taskID}/pause", s.PauseTask)
			r.Post("/{taskID}/resume", s.ResumeTask)
			r.Post("/{taskID}/confirm", s.ConfirmTask)
		})

		// Waypoints
		r.Route("/waypoints", func(r chi.Router) {
			r.Get("/", s.ListWaypoints)
			r.Post("/", s.CreateWaypoint)
			r.Get("/{waypointID}", s.GetWaypoint)
			r.Put("/{waypointID}", s.UpdateWaypoint)
			r.Delete("/{waypointID}", s.DeleteWaypoint)
		})

		// State Definitions (legacy support)
		r.Route("/state-definitions", func(r chi.Router) {
			r.Get("/", s.ListStateDefinitions)
			r.Post("/", s.CreateStateDefinition)
			r.Get("/{stateDefID}", s.GetStateDefinition)
			r.Put("/{stateDefID}", s.UpdateStateDefinition)
			r.Delete("/{stateDefID}", s.DeleteStateDefinition)
			r.Post("/{stateDefID}/deploy", s.DeployStateDefinition)
		})

		// Fleet State
		r.Route("/fleet", func(r chi.Router) {
			r.Get("/state", s.GetFleetState)
			r.Get("/summary", s.GetFleetSummary)
			r.Get("/robots/{robotID}", s.GetRobotState)
			r.Get("/agents/{agentID}/robots", s.GetAgentRobotsState)
			r.Post("/validate", s.ValidatePreconditions)
		})

		// Templates
		r.Route("/templates", func(r chi.Router) {
			r.Get("/", s.ListTemplates)
			r.Post("/", s.CreateTemplate)
			r.Get("/agents-overview", s.GetAgentsOverview)
			r.Get("/agents/{agentID}/available-templates", s.GetAvailableTemplatesForAgent)
			r.Get("/{templateID}", s.GetTemplate)
			r.Put("/{templateID}", s.UpdateTemplate)
			r.Delete("/{templateID}", s.DeleteTemplate)
			r.Get("/{templateID}/assignments", s.GetTemplateAssignments)
			r.Get("/{templateID}/compatible-agents", s.GetTemplateCompatibleAgents)
			r.Post("/{templateID}/assignments", s.AssignTemplateToAgent)
			r.Post("/{templateID}/assignments/{agentID}/deploy", s.DeployTemplateAssignment)
			r.Delete("/{templateID}/assignments/{agentID}", s.UnassignTemplate)
		})

		// Actions metadata
		r.Route("/actions", func(r chi.Router) {
			r.Get("/", s.ListActions)
			r.Get("/*", s.GetAction)
		})

		// Teach waypoint
		r.Post("/robots/{robotID}/teach", s.TeachWaypoint)

		// Block copy/paste
		r.Post("/action-graphs/{graphID}/copy-blocks", s.CopyActionGraphBlocks)
		r.Post("/action-graphs/{graphID}/paste-blocks", s.PasteActionGraphBlocks)
		r.Post("/action-graphs/{graphID}/copy", s.CopyActionGraph)

		// Agent compatible templates
		r.Get("/agents/{agentID}/compatible-templates", s.GetAgentCompatibleTemplates)

		// System/Internal endpoints
		r.Route("/system", func(r chi.Router) {
			r.Get("/cache/stats", s.GetCacheStats)
			r.Post("/cache/evict", s.EvictStaleCache)
		})
	})

	// WebSocket
	r.Get("/ws/monitor", s.HandleWebSocket)

	s.router = r
}

// Router returns the chi router
func (s *Server) Router() *chi.Mux {
	return s.router
}

// GetWebSocketHub returns the WebSocket hub for broadcasting
func (s *Server) GetWebSocketHub() *WebSocketHub {
	return s.wsHub
}
