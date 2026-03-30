package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type saveFileCategory string

const (
	saveFileCategoryTaskSet      saveFileCategory = "task_sets"
	saveFileCategoryTask         saveFileCategory = "tasks"
	saveFileCategoryPddlProfiles saveFileCategory = "pddl_profiles"
)

var saveFileNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type saveFileListItem struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	UpdatedAt string `json:"updated_at"`
}

type saveFileListResponse struct {
	Files []saveFileListItem `json:"files"`
}

type saveFileWriteRequest struct {
	FileName string          `json:"file_name"`
	Filename string          `json:"filename"`
	Payload  json.RawMessage `json:"payload"`
}

type saveFileWriteResponse struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	UpdatedAt string `json:"updated_at"`
}

func resolveSaveFilesRoot() string {
	candidates := make([]string, 0, 6)

	if envPath := strings.TrimSpace(os.Getenv("MRS_SAVE_FILES_ROOT")); envPath != "" {
		candidates = append(candidates, envPath)
	}
	if envPath := strings.TrimSpace(os.Getenv("SAVE_FILES_ROOT")); envPath != "" {
		candidates = append(candidates, envPath)
	}

	cwd, err := os.Getwd()
	if err == nil && strings.TrimSpace(cwd) != "" {
		parentDir := filepath.Clean(filepath.Join(cwd, ".."))
		if _, statErr := os.Stat(filepath.Join(parentDir, ".git")); statErr == nil {
			candidates = append(candidates, filepath.Join(parentDir, "Save_files"))
		}
		candidates = append(candidates, filepath.Join(cwd, "Save_files"))
	}

	if homeDir, homeErr := os.UserHomeDir(); homeErr == nil && strings.TrimSpace(homeDir) != "" {
		candidates = append(candidates, filepath.Join(homeDir, "Save_files"))
	}
	candidates = append(candidates, filepath.Join(os.TempDir(), "Save_files"))

	seen := make(map[string]struct{}, len(candidates))
	for _, rawPath := range candidates {
		path := strings.TrimSpace(rawPath)
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) && cwd != "" {
			path = filepath.Join(cwd, path)
		}
		path = filepath.Clean(path)
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		if canPrepareWritableDir(path) {
			return path
		}
	}

	// Final fallback (best effort)
	return filepath.Join(os.TempDir(), "Save_files")
}

func canPrepareWritableDir(dirPath string) bool {
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return false
	}
	tempFile, err := os.CreateTemp(dirPath, ".write-test-*")
	if err != nil {
		return false
	}
	name := tempFile.Name()
	_ = tempFile.Close()
	_ = os.Remove(name)
	return true
}

type saveFileDirConfig struct {
	primary string
	legacy  []string
}

func saveFileCategoryDirConfig(category saveFileCategory) (saveFileDirConfig, error) {
	switch category {
	case saveFileCategoryTaskSet:
		return saveFileDirConfig{
			primary: filepath.Join("task", "task_sets"),
			legacy:  []string{"tasks"},
		}, nil
	case saveFileCategoryTask:
		return saveFileDirConfig{
			primary: filepath.Join("task", "tasks"),
		}, nil
	case saveFileCategoryPddlProfiles:
		return saveFileDirConfig{
			primary: "pddl",
		}, nil
	default:
		return saveFileDirConfig{}, fmt.Errorf("unsupported save file category: %s", category)
	}
}

func normalizeJSONFileName(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", fmt.Errorf("file_name is required")
	}

	baseName := filepath.Base(trimmed)
	baseName = strings.ReplaceAll(baseName, "\\", "")
	baseName = saveFileNameSanitizer.ReplaceAllString(baseName, "_")
	baseName = strings.Trim(baseName, "._")

	if baseName == "" {
		return "", fmt.Errorf("invalid file_name")
	}

	if !strings.HasSuffix(strings.ToLower(baseName), ".json") {
		baseName += ".json"
	}

	return baseName, nil
}

func (s *Server) saveFileDir(category saveFileCategory) (string, error) {
	config, err := saveFileCategoryDirConfig(category)
	if err != nil {
		return "", err
	}
	dirPath := filepath.Join(s.saveFilesRoot, config.primary)
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return "", err
	}
	return dirPath, nil
}

func (s *Server) saveFileLookupDirs(category saveFileCategory) ([]string, error) {
	primaryDir, err := s.saveFileDir(category)
	if err != nil {
		return nil, err
	}

	config, err := saveFileCategoryDirConfig(category)
	if err != nil {
		return nil, err
	}

	dirs := []string{primaryDir}
	for _, legacyRelPath := range config.legacy {
		legacyDir := filepath.Join(s.saveFilesRoot, legacyRelPath)
		info, statErr := os.Stat(legacyDir)
		if statErr != nil || !info.IsDir() {
			continue
		}
		dirs = append(dirs, legacyDir)
	}

	return dirs, nil
}

func (s *Server) listSaveFiles(w http.ResponseWriter, category saveFileCategory) {
	dirPaths, err := s.saveFileLookupDirs(category)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare save file directory")
		return
	}

	filesByName := make(map[string]saveFileListItem)
	for _, dirPath := range dirPaths {
		entries, readErr := os.ReadDir(dirPath)
		if readErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to list save files")
			return
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".json") {
				continue
			}
			if _, exists := filesByName[name]; exists {
				continue
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				continue
			}
			filesByName[name] = saveFileListItem{
				Name:      name,
				SizeBytes: info.Size(),
				UpdatedAt: info.ModTime().UTC().Format(time.RFC3339),
			}
		}
	}

	files := make([]saveFileListItem, 0, len(filesByName))
	for _, file := range filesByName {
		files = append(files, file)
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].UpdatedAt == files[j].UpdatedAt {
			return files[i].Name < files[j].Name
		}
		return files[i].UpdatedAt > files[j].UpdatedAt
	})

	writeJSON(w, http.StatusOK, saveFileListResponse{Files: files})
}

func (s *Server) saveJSONFile(w http.ResponseWriter, r *http.Request, category saveFileCategory) {
	var req saveFileWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	fileName := strings.TrimSpace(req.FileName)
	if fileName == "" {
		fileName = strings.TrimSpace(req.Filename)
	}
	normalizedName, err := normalizeJSONFileName(fileName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(req.Payload) == 0 {
		writeError(w, http.StatusBadRequest, "payload is required")
		return
	}

	var payload interface{}
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "payload must be valid JSON")
		return
	}
	formattedPayload, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to serialize payload")
		return
	}
	formattedPayload = append(formattedPayload, '\n')

	dirPath, err := s.saveFileDir(category)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare save file directory")
		return
	}
	targetPath := filepath.Join(dirPath, normalizedName)
	if err := os.WriteFile(targetPath, formattedPayload, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write save file")
		return
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save file written but metadata read failed")
		return
	}

	writeJSON(w, http.StatusOK, saveFileWriteResponse{
		Name:      normalizedName,
		SizeBytes: info.Size(),
		UpdatedAt: info.ModTime().UTC().Format(time.RFC3339),
	})
}

func (s *Server) loadJSONFile(w http.ResponseWriter, fileName string, category saveFileCategory) {
	normalizedName, err := normalizeJSONFileName(fileName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	dirPaths, err := s.saveFileLookupDirs(category)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare save file directory")
		return
	}

	var raw []byte
	found := false
	for _, dirPath := range dirPaths {
		targetPath := filepath.Join(dirPath, normalizedName)
		raw, err = os.ReadFile(targetPath)
		if err == nil {
			found = true
			break
		}
		if !os.IsNotExist(err) {
			writeError(w, http.StatusInternalServerError, "failed to read save file")
			return
		}
	}

	if !found {
		writeError(w, http.StatusNotFound, "save file not found")
		return
	}

	var payload interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		writeError(w, http.StatusInternalServerError, "save file contains invalid json")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":    normalizedName,
		"payload": payload,
	})
}

func (s *Server) ListTaskSetSaveFiles(w http.ResponseWriter, _ *http.Request) {
	s.listSaveFiles(w, saveFileCategoryTaskSet)
}

func (s *Server) ListTaskSaveFiles(w http.ResponseWriter, _ *http.Request) {
	s.listSaveFiles(w, saveFileCategoryTask)
}

func (s *Server) SaveTaskSetFile(w http.ResponseWriter, r *http.Request) {
	s.saveJSONFile(w, r, saveFileCategoryTaskSet)
}

func (s *Server) SaveTaskFile(w http.ResponseWriter, r *http.Request) {
	s.saveJSONFile(w, r, saveFileCategoryTask)
}

func (s *Server) LoadTaskSetFile(w http.ResponseWriter, r *http.Request) {
	s.loadJSONFile(w, chiURLParam(r, "fileName"), saveFileCategoryTaskSet)
}

func (s *Server) LoadTaskFile(w http.ResponseWriter, r *http.Request) {
	s.loadJSONFile(w, chiURLParam(r, "fileName"), saveFileCategoryTask)
}

func (s *Server) ListPddlProfileSaveFiles(w http.ResponseWriter, _ *http.Request) {
	s.listSaveFiles(w, saveFileCategoryPddlProfiles)
}

func (s *Server) SavePddlProfileFile(w http.ResponseWriter, r *http.Request) {
	s.saveJSONFile(w, r, saveFileCategoryPddlProfiles)
}

func (s *Server) LoadPddlProfileFile(w http.ResponseWriter, r *http.Request) {
	s.loadJSONFile(w, chiURLParam(r, "fileName"), saveFileCategoryPddlProfiles)
}
