package server

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mmatz-101/realestate-leads/internal/browser"
	"github.com/mmatz-101/realestate-leads/internal/jobs"
)

// Server is the HTTP server for the web UI and API.
type Server struct {
	session   *browser.SessionManager
	runner    *jobs.Runner
	port      int
	uploadDir string
	outputDir string
}

// NewServer creates a new HTTP server.
func NewServer(session *browser.SessionManager, runner *jobs.Runner, port int) *Server {
	uploadDir := filepath.Join(os.TempDir(), "realestate-leads", "uploads")
	outputDir := filepath.Join(os.TempDir(), "realestate-leads", "output")
	os.MkdirAll(uploadDir, 0755)
	os.MkdirAll(outputDir, 0755)

	return &Server{
		session:   session,
		runner:    runner,
		port:      port,
		uploadDir: uploadDir,
		outputDir: outputDir,
	}
}

// Start begins serving on the configured port.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/login-confirm", s.handleLoginConfirm)
	mux.HandleFunc("POST /api/upload", s.handleUpload)
	mux.HandleFunc("POST /api/start", s.handleStartJob)
	mux.HandleFunc("POST /api/job/{id}/cancel", s.handleCancelJob)
	mux.HandleFunc("POST /api/job/{id}/continue", s.handleContinueToStage2)
	mux.HandleFunc("GET /api/job/{id}", s.handleJobStatus)
	mux.HandleFunc("GET /api/job/{id}/download", s.handleDownload)
	mux.HandleFunc("GET /api/events", s.handleSSE)

	// Serve the web UI
	mux.Handle("GET /", http.FileServer(http.Dir("web")))

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("[server] Listening on http://localhost%s", addr)
	return http.ListenAndServe(addr, mux)
}

// handleStatus returns the current session state.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]any{
		"loggedIn": s.session.IsLoggedIn(),
	})
}

// handleLoginConfirm is called by the UI when the user says they've logged in.
func (s *Server) handleLoginConfirm(w http.ResponseWriter, r *http.Request) {
	s.session.MarkLoggedIn()
	json.NewEncoder(w).Encode(map[string]any{
		"ok": true,
	})
}

// handleUpload accepts a CSV file upload.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(32 << 20) // 32MB max

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "No file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	destPath := filepath.Join(s.uploadDir, header.Filename)
	dest, err := os.Create(destPath)
	if err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	defer dest.Close()

	if _, err := io.Copy(dest, file); err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	// Read headers from the CSV so the UI can show column picker.
	headers, err := readCSVHeaders(destPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read CSV: %v", err), http.StatusBadRequest)
		return
	}

	log.Printf("[server] Uploaded: %s (%d bytes)", header.Filename, header.Size)

	json.NewEncoder(w).Encode(map[string]any{
		"fileName": header.Filename,
		"path":     destPath,
		"headers":  headers,
	})
}

// handleStartJob kicks off a search job.
func (s *Server) handleStartJob(w http.ResponseWriter, r *http.Request) {
	if !s.session.IsLoggedIn() {
		http.Error(w, "Not logged in", http.StatusUnauthorized)
		return
	}

	var req struct {
		FilePath string          `json:"filePath"`
		Mode     jobs.JobMode    `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Default to forewarn mode if not specified
	if req.Mode == "" {
		req.Mode = jobs.JobModeForewarn
	}

	jobID := fmt.Sprintf("job-%d", time.Now().UnixMilli())
	job, err := s.runner.StartJob(jobID, req.FilePath, s.outputDir, req.Mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("[server] Started job %s (mode: %s): %s", jobID, req.Mode, req.FilePath)

	json.NewEncoder(w).Encode(map[string]any{
		"jobId":       job.ID,
		"mode":        job.Mode,
		"total":       job.TotalRows,
		"fileName":    job.FileName,
		"totalStages": job.TotalStages,
	})
}

// handleCancelJob cancels a running job.
func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.runner.CancelJob(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"ok": true,
	})
}

// handleJobStatus returns progress for a specific job.
func (s *Server) handleJobStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	progress, ok := s.runner.GetJob(id)
	if !ok {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(progress)
}

// handleDownload serves the result CSV for a completed job.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	progress, ok := s.runner.GetJob(id)
	if !ok {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}
	if progress.Status != jobs.StatusComplete && progress.Status != jobs.StatusCancelled {
		http.Error(w, "Job not complete", http.StatusBadRequest)
		return
	}

	// Check if a specific stage is requested via query parameter
	stageParam := r.URL.Query().Get("stage")

	// Determine which file to download based on mode, stage, and query param
	var outputPath string
	var filename string

	if stageParam == "1" {
		// Explicitly requesting stage 1 results
		outputPath = filepath.Join(s.outputDir, id+"-stage1-results.csv")
		filename = id + "-stage1-results.csv"
	} else if progress.Mode == jobs.JobModePreview {
		// Preview mode: download stage 1 results
		outputPath = filepath.Join(s.outputDir, id+"-stage1-results.csv")
		filename = id + "-stage1-results.csv"
	} else if progress.Mode == jobs.JobModeFull && progress.CurrentStage == 1 {
		// Full mode, stage 1 only (if cancelled)
		outputPath = filepath.Join(s.outputDir, id+"-stage1-results.csv")
		filename = id + "-stage1-results.csv"
	} else if progress.Mode == jobs.JobModeFull && progress.CurrentStage == 2 {
		// Full mode, stage 2 complete
		outputPath = filepath.Join(s.outputDir, id+"-stage2-results.csv")
		filename = id + "-stage2-results.csv"
	} else {
		// Forewarn-only mode
		outputPath = filepath.Join(s.outputDir, id+"-results.csv")
		filename = id + "-results.csv"
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Type", "text/csv")
	http.ServeFile(w, r, outputPath)
}

// handleSSE streams job progress via Server-Sent Events.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.runner.Subscribe()
	defer s.runner.Unsubscribe(ch)

	for {
		select {
		case progress, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(progress)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

		case <-r.Context().Done():
			return
		}
	}
}

// handleContinueToStage2 takes a preview job and continues it to stage 2 (Forewarn enrichment).
func (s *Server) handleContinueToStage2(w http.ResponseWriter, r *http.Request) {
	if !s.session.IsLoggedIn() {
		http.Error(w, "Not logged in", http.StatusUnauthorized)
		return
	}

	id := r.PathValue("id")
	progress, ok := s.runner.GetJob(id)
	if !ok {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Verify the job is in preview mode and complete
	if progress.Mode != jobs.JobModePreview {
		http.Error(w, "Job is not in preview mode", http.StatusBadRequest)
		return
	}
	if progress.Status != jobs.StatusComplete {
		http.Error(w, "Job is not complete", http.StatusBadRequest)
		return
	}

	// Use the output CSV from the preview job as input for stage 2
	previewOutputPath := filepath.Join(s.outputDir, id+"-results.csv")

	// Create a new job ID for stage 2
	newJobID := fmt.Sprintf("job-%d", time.Now().UnixMilli())

	// Start the new job in forewarn-only mode
	job, err := s.runner.StartJob(newJobID, previewOutputPath, s.outputDir, jobs.JobModeForewarn)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("[server] Continuing job %s to stage 2 as %s", id, newJobID)

	json.NewEncoder(w).Encode(map[string]any{
		"jobId":       job.ID,
		"mode":        job.Mode,
		"total":       job.TotalRows,
		"fileName":    job.FileName,
		"totalStages": job.TotalStages,
	})
}

// readCSVHeaders reads just the first row of a CSV to get column names.
func readCSVHeaders(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	headers, err := reader.Read()
	if err != nil {
		return nil, err
	}
	return headers, nil
}
