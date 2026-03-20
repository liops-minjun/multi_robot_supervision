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
	planExecutor    *executor.PlanExecutor
	realtimePddl    *RealtimePddlManager
	wsHub           *WebSocketHub
	quicHandler     *fleetgrpc.RawQUICHandler
	definitionsPath string
}

// NewServer creates a new API server
func NewServer(repo *db.Repository, stateManager *state.GlobalStateManager, scheduler *executor.Scheduler, quicHandler *fleetgrpc.RawQUICHandler, definitionsPath string) *Server {
	wsHub := NewWebSocketHub()
	s := &Server{
		repo:            repo,
		stateManager:    stateManager,
		scheduler:       scheduler,
		wsHub:           wsHub,
		quicHandler:     quicHandler,
		definitionsPath: definitionsPath,
	}
	s.planExecutor = executor.NewPlanExecutor(scheduler, stateManager, repo, func(msg interface{}) {
		wsHub.Broadcast(msg)
	})
	s.realtimePddl = NewRealtimePddlManager(s)
	if quicHandler != nil {
		quicHandler.SetPlanningStateCallback(func(agentID string, values map[string]string) {
			// Agent telemetry runtime-state updates are ephemeral.
			// Keep a short TTL so stale values disappear automatically.
			_ = s.realtimePddl.UpsertRuntimeStateByAgent(agentID, "agent:"+agentID, values, 5.0)
		})
	}

	s.setupRouter()

	// Start WebSocket hub (handles client registration/unregistration)
	go s.wsHub.Run()

	// Start centralized broadcast loop (single goroutine for all clients)
	// This is more efficient than per-client goroutines
	go s.StartBroadcastLoop()

	// Start lock cleanup background worker (cleans up expired edit locks)
	s.StartLockCleanup()

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
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Session-ID"},
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
			r.Get("/changed", s.GetCapabilitiesChangedSince) // Incremental sync endpoint
			r.Get("/by-category/{category}", s.GetCapabilitiesByCategoryAPI)
			r.Get("/{capabilityID}", s.GetCapabilityByID)    // Get single capability
			r.Patch("/{capabilityID}", s.UpdateCapabilityMetadata) // Update capability metadata
			r.Get("/action-type/*", s.GetCapabilitiesByActionType) // Moved to explicit path
		})

		// Behavior Trees
		r.Route("/behavior-trees", func(r chi.Router) {
			r.Get("/", s.ListBehaviorTrees)
			r.Post("/", s.CreateBehaviorTree)
			r.Post("/import", s.ImportBehaviorTree) // Import canonical graph (before {graphID} routes)
			r.Get("/{graphID}", s.GetBehaviorTree)
			r.Put("/{graphID}", s.UpdateBehaviorTree)
			r.Delete("/{graphID}", s.DeleteBehaviorTree)
			r.Get("/{graphID}/check-executability", s.CheckExecutability) // Safety check before execution
			r.Post("/{graphID}/execute", s.ExecuteBehaviorTree)
			r.Post("/{graphID}/execute-multi", s.ExecuteMultiBehaviorTree) // Multi-agent simultaneous execution
			r.Post("/{graphID}/validate", s.ValidateBehaviorTree)
			r.Get("/{graphID}/export", s.ExportBehaviorTree) // Export canonical graph

			// Edit lock endpoints (concurrent editing prevention)
			r.Post("/{graphID}/lock", s.AcquireBehaviorTreeLock)
			r.Delete("/{graphID}/lock", s.ReleaseBehaviorTreeLock)
			r.Get("/{graphID}/lock", s.GetBehaviorTreeLockStatus)
			r.Post("/{graphID}/lock/heartbeat", s.HeartbeatBehaviorTreeLock)
			r.Delete("/{graphID}/lock/force", s.ForceReleaseBehaviorTreeLock)

			// Canonical Graph endpoints (new graph-optimized format)
			r.Get("/{graphID}/canonical", s.GetCanonicalGraph)
			r.Post("/{graphID}/validate-canonical", s.ValidateCanonicalGraph)
			r.Post("/{graphID}/deploy/{agentID}", s.DeployBehaviorTreeToAgent)
		})

		// Agents
		r.Route("/agents", func(r chi.Router) {
			r.Get("/", s.ListAgents)
			r.Post("/", s.CreateAgent)
			r.Get("/connection-status", s.GetAgentConnectionStatus) // Heartbeat monitoring for all agents
			r.Get("/{agentID}", s.GetAgent)
			r.Patch("/{agentID}", s.UpdateAgent)   // Update agent (rename)
			r.Delete("/{agentID}", s.DeleteAgent)
			r.Post("/{agentID}/capability-template", s.SaveAgentCapabilityTemplate)
			r.Get("/{agentID}/capabilities", s.GetAgentCapabilities)
			r.Get("/{agentID}/connection-status", s.GetSingleAgentConnectionStatus) // Heartbeat monitoring for single agent
			r.Get("/{agentID}/logs", s.GetAgentLogs)                                // Execution logs for agent
			r.Post("/{agentID}/reset-state", s.ResetAgentState)                     // Reset agent state to idle

			r.Route("/{agentID}/behavior-trees", func(r chi.Router) {
				r.Get("/", s.ListAgentBehaviorTrees)
				r.Post("/", s.AssignBehaviorTree)
				r.Get("/{graphID}", s.GetAgentBehaviorTree)
				r.Delete("/{graphID}", s.RemoveAgentBehaviorTree)
				r.Post("/{graphID}/deploy", s.DeployBehaviorTree)
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
			r.Get("/{taskID}/logs", s.GetTaskLogs)
			r.Get("/{taskID}/precondition-status", s.GetTaskPreconditionStatus)
		})

		// Task Logs (execution streaming logs)
		r.Route("/logs", func(r chi.Router) {
			r.Get("/", s.GetRecentLogs)
			r.Get("/stats", s.GetLogStats)
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
			r.Get("/robots/{robotID}/telemetry", s.GetRobotTelemetry)
			r.Get("/robots/{robotID}/telemetry/joint-state", s.GetRobotJointState)
			r.Get("/robots/{robotID}/telemetry/odometry", s.GetRobotOdometry)
			r.Get("/robots/{robotID}/telemetry/transforms", s.GetRobotTransforms)
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
			r.Patch("/{templateID}/identity", s.UpdateTemplateIdentity)
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
		r.Post("/behavior-trees/{graphID}/copy-blocks", s.CopyBehaviorTreeBlocks)
		r.Post("/behavior-trees/{graphID}/paste-blocks", s.PasteBehaviorTreeBlocks)
		r.Post("/behavior-trees/{graphID}/copy", s.CopyBehaviorTree)

		// Agent compatible templates
		r.Get("/agents/{agentID}/compatible-templates", s.GetAgentCompatibleTemplates)

		// PDDL Task Distribution
		r.Route("/pddl", func(r chi.Router) {
			r.Route("/problems", func(r chi.Router) {
				r.Get("/", s.ListPlanningProblems)
				r.Post("/", s.CreatePlanningProblem)
				r.Get("/{problemID}", s.GetPlanningProblem)
				r.Delete("/{problemID}", s.DeletePlanningProblem)
				r.Post("/{problemID}/solve", s.SolvePlanningProblem)
				r.Post("/{problemID}/execute", s.ExecutePlan)
			})
			r.Post("/preview", s.PreviewDistribution)

			// Plan Executions
			r.Route("/executions", func(r chi.Router) {
				r.Get("/", s.ListPlanExecutions)
				r.Get("/{executionID}", s.GetPlanExecution)
				r.Post("/{executionID}/cancel", s.CancelPlanExecution)
			})

			r.Route("/realtime-sessions", func(r chi.Router) {
				r.Get("/", s.ListRealtimeSessions)
				r.Post("/", s.StartRealtimeSession)
				r.Get("/{sessionID}", s.GetRealtimeSession)
				r.Post("/{sessionID}/reset-state", s.ResetRealtimeSessionState)
				r.Post("/{sessionID}/stop", s.StopRealtimeSession)
			})

			// Resource allocations
			r.Get("/resources", s.GetPlanResources)
		})

		// Task Distributors
		r.Route("/task-distributors", func(r chi.Router) {
			r.Get("/", s.ListTaskDistributors)
			r.Post("/", s.CreateTaskDistributor)
			r.Get("/{distributorID}", s.GetTaskDistributor)
			r.Get("/{distributorID}/full", s.GetTaskDistributorFull)
			r.Put("/{distributorID}", s.UpdateTaskDistributor)
			r.Delete("/{distributorID}", s.DeleteTaskDistributor)
			r.Get("/{distributorID}/states", s.ListTaskDistributorStates)
			r.Post("/{distributorID}/states", s.CreateTaskDistributorState)
			r.Put("/{distributorID}/states/{stateID}", s.UpdateTaskDistributorState)
			r.Delete("/{distributorID}/states/{stateID}", s.DeleteTaskDistributorState)
			r.Get("/{distributorID}/resources", s.ListTaskDistributorResources)
			r.Post("/{distributorID}/resources", s.CreateTaskDistributorResource)
			r.Put("/{distributorID}/resources/{resourceID}", s.UpdateTaskDistributorResource)
			r.Delete("/{distributorID}/resources/{resourceID}", s.DeleteTaskDistributorResource)
			r.Get("/{distributorID}/runtime-state", s.ListTaskDistributorRuntimeState)
			r.Post("/{distributorID}/runtime-state", s.UpsertTaskDistributorRuntimeState)
			r.Delete("/{distributorID}/runtime-state", s.ClearTaskDistributorRuntimeState)
		})

		// System/Internal endpoints
		r.Route("/system", func(r chi.Router) {
			r.Get("/cache/stats", s.GetCacheStats)
			r.Post("/cache/evict", s.EvictStaleCache)
			r.Get("/states", s.GetSystemStates) // Get predefined system states
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
