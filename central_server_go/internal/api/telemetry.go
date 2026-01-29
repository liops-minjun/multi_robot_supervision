package api

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// GetRobotTelemetry returns current telemetry for a robot
// GET /api/fleet/robots/{robotID}/telemetry
func (s *Server) GetRobotTelemetry(w http.ResponseWriter, r *http.Request) {
	robotID := chi.URLParam(r, "robotID")

	telemetry := s.stateManager.GetRobotTelemetry(robotID)
	if telemetry == nil {
		// Debug: log the request
		log.Printf("[Telemetry API] No telemetry for robot %s", robotID)
		writeError(w, http.StatusNotFound, "Telemetry not available for robot")
		return
	}

	writeJSON(w, http.StatusOK, telemetry)
}

// GetRobotJointState returns current joint state for a robot
// GET /api/fleet/robots/{robotID}/telemetry/joint-state
func (s *Server) GetRobotJointState(w http.ResponseWriter, r *http.Request) {
	robotID := chi.URLParam(r, "robotID")

	jointState := s.stateManager.GetRobotJointState(robotID)
	if jointState == nil {
		writeError(w, http.StatusNotFound, "JointState not available")
		return
	}

	writeJSON(w, http.StatusOK, jointState)
}

// GetRobotOdometry returns current odometry for a robot
// GET /api/fleet/robots/{robotID}/telemetry/odometry
func (s *Server) GetRobotOdometry(w http.ResponseWriter, r *http.Request) {
	robotID := chi.URLParam(r, "robotID")

	odometry := s.stateManager.GetRobotOdometry(robotID)
	if odometry == nil {
		writeError(w, http.StatusNotFound, "Odometry not available")
		return
	}

	writeJSON(w, http.StatusOK, odometry)
}

// GetRobotTransforms returns current TF transforms for a robot
// GET /api/fleet/robots/{robotID}/telemetry/transforms
func (s *Server) GetRobotTransforms(w http.ResponseWriter, r *http.Request) {
	robotID := chi.URLParam(r, "robotID")

	transforms := s.stateManager.GetRobotTransforms(robotID)
	if transforms == nil || len(transforms) == 0 {
		writeError(w, http.StatusNotFound, "Transforms not available")
		return
	}

	writeJSON(w, http.StatusOK, transforms)
}
