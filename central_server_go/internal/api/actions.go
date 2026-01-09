package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// ============================================
// Actions Metadata API
// ============================================

// ActionField represents an action field definition
type ActionField struct {
	Name          string                 `json:"name" yaml:"name"`
	Type          string                 `json:"type" yaml:"type"`
	Default       interface{}            `json:"default,omitempty" yaml:"default"`
	IsArray       bool                   `json:"is_array"`
	IsConstant    bool                   `json:"is_constant,omitempty"`
	ConstantValue interface{}            `json:"constant_value,omitempty"`
	Constants     map[string]interface{} `json:"constants,omitempty" yaml:"constants"`
}

// ActionDefinition represents an action definition from YAML
type ActionDefinition struct {
	Package        string        `json:"package" yaml:"package"`
	Name           string        `json:"name" yaml:"name"`
	FullName       string        `json:"full_name"`
	Server         string        `json:"server" yaml:"server"`
	Category       string        `json:"category"` // Directory name for organization
	Description    string        `json:"description,omitempty" yaml:"description"`
	GoalFields     []ActionField `json:"goal_fields,omitempty" yaml:"goal_fields"`
	ResultFields   []ActionField `json:"result_fields,omitempty" yaml:"result_fields"`
	FeedbackFields []ActionField `json:"feedback_fields,omitempty" yaml:"feedback_fields"`
}

// ActionListItem represents a summarized action for listing
type ActionListItem struct {
	FullName           string `json:"full_name"`
	Package            string `json:"package"`
	Name               string `json:"name"`
	Server             string `json:"server"`
	Category           string `json:"category,omitempty"`
	Description        string `json:"description,omitempty"`
	GoalFieldCount     int    `json:"goal_field_count"`
	ResultFieldCount   int    `json:"result_field_count"`
	FeedbackFieldCount int    `json:"feedback_field_count"`
}

// actionsYAML represents the structure of actions.yaml
type actionsYAML struct {
	Actions []struct {
		Package        string `yaml:"package"`
		Name           string `yaml:"name"`
		Server         string `yaml:"server"`
		Description    string `yaml:"description"`
		GoalFields     []struct {
			Name      string                 `yaml:"name"`
			Type      string                 `yaml:"type"`
			Default   interface{}            `yaml:"default"`
			Constants map[string]interface{} `yaml:"constants"`
		} `yaml:"goal_fields"`
		ResultFields []struct {
			Name      string                 `yaml:"name"`
			Type      string                 `yaml:"type"`
			Constants map[string]interface{} `yaml:"constants"`
		} `yaml:"result_fields"`
		FeedbackFields []struct {
			Name string `yaml:"name"`
			Type string `yaml:"type"`
		} `yaml:"feedback_fields"`
	} `yaml:"actions"`
}

// ActionLoader loads and caches action definitions
type ActionLoader struct {
	definitionsPath   string
	actions           map[string]*ActionDefinition
	actionsByCategory map[string]map[string]*ActionDefinition
	loaded            bool
}

var globalActionLoader *ActionLoader

// GetActionLoader returns the global action loader
func GetActionLoader(definitionsPath string) *ActionLoader {
	if globalActionLoader == nil {
		globalActionLoader = &ActionLoader{
			definitionsPath:   definitionsPath,
			actions:           make(map[string]*ActionDefinition),
			actionsByCategory: make(map[string]map[string]*ActionDefinition),
		}
	}
	return globalActionLoader
}

// LoadAll loads all action definitions from YAML files
func (l *ActionLoader) LoadAll() error {
	actionsPath := filepath.Join(l.definitionsPath, "actions")

	if _, err := os.Stat(actionsPath); os.IsNotExist(err) {
		// No actions directory, not an error
		l.loaded = true
		return nil
	}

	entries, err := os.ReadDir(actionsPath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			category := entry.Name()
			actionsYAMLPath := filepath.Join(actionsPath, category, "actions.yaml")

			if _, err := os.Stat(actionsYAMLPath); err == nil {
				l.loadActionsYAML(category, actionsYAMLPath)
			}
		}
	}

	l.loaded = true
	return nil
}

// loadActionsYAML loads actions from a single YAML file
func (l *ActionLoader) loadActionsYAML(category, yamlPath string) error {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return err
	}

	var actionsData actionsYAML
	if err := yaml.Unmarshal(data, &actionsData); err != nil {
		return err
	}

	if l.actionsByCategory[category] == nil {
		l.actionsByCategory[category] = make(map[string]*ActionDefinition)
	}

	for _, a := range actionsData.Actions {
		fullName := a.Package + "/" + a.Name

		goalFields := make([]ActionField, 0, len(a.GoalFields))
		for _, f := range a.GoalFields {
			isArray := strings.Contains(f.Type, "[]")
			goalFields = append(goalFields, ActionField{
				Name:      f.Name,
				Type:      strings.ReplaceAll(f.Type, "[]", ""),
				Default:   f.Default,
				IsArray:   isArray,
				Constants: f.Constants,
			})
		}

		resultFields := make([]ActionField, 0, len(a.ResultFields))
		for _, f := range a.ResultFields {
			isArray := strings.Contains(f.Type, "[]")
			resultFields = append(resultFields, ActionField{
				Name:      f.Name,
				Type:      strings.ReplaceAll(f.Type, "[]", ""),
				IsArray:   isArray,
				Constants: f.Constants,
			})
		}

		feedbackFields := make([]ActionField, 0, len(a.FeedbackFields))
		for _, f := range a.FeedbackFields {
			isArray := strings.Contains(f.Type, "[]")
			feedbackFields = append(feedbackFields, ActionField{
				Name:    f.Name,
				Type:    strings.ReplaceAll(f.Type, "[]", ""),
				IsArray: isArray,
			})
		}

		action := &ActionDefinition{
			Package:        a.Package,
			Name:           a.Name,
			FullName:       fullName,
			Server:         a.Server,
			Category:       category,
			Description:    a.Description,
			GoalFields:     goalFields,
			ResultFields:   resultFields,
			FeedbackFields: feedbackFields,
		}

		l.actions[fullName] = action
		l.actionsByCategory[category][fullName] = action
	}

	return nil
}

// GetAction returns an action by full name
func (l *ActionLoader) GetAction(actionType string) *ActionDefinition {
	if !l.loaded {
		l.LoadAll()
	}
	return l.actions[actionType]
}

// GetActionsForCategory returns all actions for a category
func (l *ActionLoader) GetActionsForCategory(category string) map[string]*ActionDefinition {
	if !l.loaded {
		l.LoadAll()
	}
	return l.actionsByCategory[category]
}

// ListAll returns all actions as list items
func (l *ActionLoader) ListAll() []ActionListItem {
	if !l.loaded {
		l.LoadAll()
	}

	result := make([]ActionListItem, 0, len(l.actions))
	for _, action := range l.actions {
		result = append(result, ActionListItem{
			FullName:           action.FullName,
			Package:            action.Package,
			Name:               action.Name,
			Server:             action.Server,
			Category:           action.Category,
			Description:        action.Description,
			GoalFieldCount:     len(action.GoalFields),
			ResultFieldCount:   len(action.ResultFields),
			FeedbackFieldCount: len(action.FeedbackFields),
		})
	}
	return result
}

// ListForCategory returns all actions for a category as list items
func (l *ActionLoader) ListForCategory(category string) []ActionListItem {
	actions := l.GetActionsForCategory(category)
	result := make([]ActionListItem, 0, len(actions))
	for _, action := range actions {
		result = append(result, ActionListItem{
			FullName:           action.FullName,
			Package:            action.Package,
			Name:               action.Name,
			Server:             action.Server,
			Category:           action.Category,
			Description:        action.Description,
			GoalFieldCount:     len(action.GoalFields),
			ResultFieldCount:   len(action.ResultFields),
			FeedbackFieldCount: len(action.FeedbackFields),
		})
	}
	return result
}

// ============================================
// API Endpoints
// ============================================

// ListActions returns all available action definitions
func (s *Server) ListActions(w http.ResponseWriter, r *http.Request) {
	loader := GetActionLoader(s.definitionsPath)
	actions := loader.ListAll()
	writeJSON(w, http.StatusOK, actions)
}

// ListActionsForCategory returns actions for a specific category
func (s *Server) ListActionsForCategory(w http.ResponseWriter, r *http.Request) {
	category := chi.URLParam(r, "category")

	loader := GetActionLoader(s.definitionsPath)
	actions := loader.ListForCategory(category)

	if len(actions) == 0 {
		writeError(w, http.StatusNotFound, "No actions found for category: "+category)
		return
	}

	writeJSON(w, http.StatusOK, actions)
}

// GetAction returns action definition details
func (s *Server) GetAction(w http.ResponseWriter, r *http.Request) {
	actionType := chi.URLParam(r, "*")

	loader := GetActionLoader(s.definitionsPath)
	action := loader.GetAction(actionType)

	if action == nil {
		writeError(w, http.StatusNotFound, "Action not found: "+actionType)
		return
	}

	writeJSON(w, http.StatusOK, action)
}
