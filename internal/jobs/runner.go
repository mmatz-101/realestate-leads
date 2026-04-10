package jobs

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mmatz-101/realestate-leads/internal/browser"
	"github.com/mmatz-101/realestate-leads/internal/comptroller"
)

// Status represents the state of a job.
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusComplete  Status = "complete"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// JobMode represents the processing mode for a job.
type JobMode string

const (
	JobModePreview  JobMode = "preview"  // Comptroller only
	JobModeFull     JobMode = "full"     // Comptroller + Forewarn
	JobModeForewarn JobMode = "forewarn" // Forewarn only (skip comptroller)
)

// Job tracks a single CSV processing run.
type Job struct {
	ID           string
	Status       Status
	Mode         JobMode // preview, full, or forewarn
	FileName     string
	TotalRows    int
	Processed    int
	Found        int
	Skipped      int
	Errors       int
	Results      []browser.SearchResult
	StartedAt    time.Time
	CompletedAt  time.Time
	OutputPath   string
	CurrentStage int // For multi-stage jobs (1 or 2)
	TotalStages  int // 1 for preview/forewarn, 2 for full
	mu           sync.RWMutex
	cancelCh     chan struct{}
}

// Progress returns a snapshot of the job's progress.
type Progress struct {
	ID           string  `json:"id"`
	Status       Status  `json:"status"`
	Mode         JobMode `json:"mode"`
	FileName     string  `json:"fileName"`
	Total        int     `json:"total"`
	Processed    int     `json:"processed"`
	Found        int     `json:"found"`
	Skipped      int     `json:"skipped"`
	Errors       int     `json:"errors"`
	Percent      float64 `json:"percent"`
	CurrentStage int     `json:"currentStage"`
	TotalStages  int     `json:"totalStages"`
	StageName    string  `json:"stageName"`
}

func (j *Job) progress() Progress {
	j.mu.RLock()
	defer j.mu.RUnlock()

	pct := 0.0
	if j.TotalRows > 0 {
		pct = float64(j.Processed) / float64(j.TotalRows) * 100
	}

	// Determine stage name
	stageName := ""
	switch j.Mode {
	case JobModePreview:
		stageName = "Comptroller Lookup"
	case JobModeForewarn:
		stageName = "Forewarn Enrichment"
	case JobModeFull:
		if j.CurrentStage == 1 {
			stageName = "Comptroller Lookup"
		} else if j.CurrentStage == 2 {
			stageName = "Forewarn Enrichment"
		}
	}

	return Progress{
		ID:           j.ID,
		Status:       j.Status,
		Mode:         j.Mode,
		FileName:     j.FileName,
		Total:        j.TotalRows,
		Processed:    j.Processed,
		Found:        j.Found,
		Skipped:      j.Skipped,
		Errors:       j.Errors,
		Percent:      pct,
		CurrentStage: j.CurrentStage,
		TotalStages:  j.TotalStages,
		StageName:    stageName,
	}
}

// Runner manages job execution.
type Runner struct {
	mu                sync.RWMutex
	jobs              map[string]*Job
	forewarnSearcher  *browser.Searcher
	comptrollerClient *comptroller.Searcher

	// subscribers for SSE progress updates
	subMu       sync.RWMutex
	subscribers map[chan Progress]struct{}
}

// NewRunner creates a job runner with the given searchers.
func NewRunner(forewarnSearcher *browser.Searcher, comptrollerSearcher *comptroller.Searcher) *Runner {
	return &Runner{
		jobs:              make(map[string]*Job),
		forewarnSearcher:  forewarnSearcher,
		comptrollerClient: comptrollerSearcher,
		subscribers:       make(map[chan Progress]struct{}),
	}
}

// Subscribe returns a channel that receives progress updates.
func (r *Runner) Subscribe() chan Progress {
	ch := make(chan Progress, 50)
	r.subMu.Lock()
	r.subscribers[ch] = struct{}{}
	r.subMu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber.
func (r *Runner) Unsubscribe(ch chan Progress) {
	r.subMu.Lock()
	delete(r.subscribers, ch)
	r.subMu.Unlock()
	close(ch)
}

func (r *Runner) broadcast(p Progress) {
	r.subMu.RLock()
	defer r.subMu.RUnlock()

	// Debug: log what we're broadcasting
	if p.Status == StatusComplete || p.Status == StatusFailed || p.Status == StatusCancelled {
		log.Printf("[runner] Broadcasting final status: %s (mode: %s, stage: %d/%d, processed: %d) to %d subscribers",
			p.Status, p.Mode, p.CurrentStage, p.TotalStages, p.Processed, len(r.subscribers))
	}

	for ch := range r.subscribers {
		select {
		case ch <- p:
		default:
			// subscriber too slow, skip
			log.Printf("[runner] WARNING: Subscriber too slow, skipped message")
		}
	}
}

// StartJob parses the CSV and kicks off searches in the background.
func (r *Runner) StartJob(id, filePath, outputDir string, mode JobMode) (*Job, error) {
	// Default to forewarn mode if not specified
	if mode == "" {
		mode = JobModeForewarn
	}
	rows, headers, err := readCSV(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %w", err)
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("CSV has no data rows")
	}

	// Verify required columns exist based on mode.
	var required []string
	switch mode {
	case JobModePreview, JobModeFull:
		// Preview and Full modes need Business Name for comptroller lookup
		required = []string{"Business Name"}
	case JobModeForewarn:
		// Forewarn-only mode needs First Name and Last Name
		required = []string{"First Name", "Last Name"}
	}

	for _, col := range required {
		found := false
		for _, h := range headers {
			if h == col {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("required column %q not found in CSV (available: %v)", col, headers)
		}
	}

	outputPath := fmt.Sprintf("%s/%s-results.csv", outputDir, id)

	// Determine stages based on mode
	totalStages := 1
	if mode == JobModeFull {
		totalStages = 2
	}

	job := &Job{
		ID:           id,
		Status:       StatusRunning,
		Mode:         mode,
		FileName:     filePath,
		TotalRows:    len(rows),
		StartedAt:    time.Now(),
		OutputPath:   outputPath,
		CurrentStage: 1,
		TotalStages:  totalStages,
		cancelCh:     make(chan struct{}),
	}

	r.mu.Lock()
	r.jobs[id] = job
	r.mu.Unlock()

	go r.runJob(job, rows, headers)

	return job, nil
}

// CancelJob stops a running job.
func (r *Runner) CancelJob(id string) error {
	r.mu.RLock()
	job, ok := r.jobs[id]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("job not found")
	}

	job.mu.Lock()
	if job.Status != StatusRunning {
		job.mu.Unlock()
		return fmt.Errorf("job is not running (status: %s)", job.Status)
	}
	job.mu.Unlock()

	close(job.cancelCh)
	log.Printf("[runner]   Job %s cancelled by user", id)
	return nil
}

func (r *Runner) runJob(job *Job, rows []map[string]string, headers []string) {
	// Determine which stages to run based on mode
	switch job.Mode {
	case JobModePreview:
		// Stage 1 only: Comptroller lookup
		r.runComptrollerStage(job, rows, headers)
	case JobModeForewarn:
		// Stage 1 only: Forewarn enrichment (skip comptroller)
		r.runForewarnStage(job, rows, headers)
	case JobModeFull:
		// Stage 1: Comptroller lookup
		job.CurrentStage = 1
		r.runComptrollerStage(job, rows, headers)

		// Check if cancelled after stage 1
		select {
		case <-job.cancelCh:
			return
		default:
		}

		// Stage 2: Forewarn enrichment using stage 1 results
		job.mu.Lock()
		job.CurrentStage = 2
		// Reset counters and clear results for stage 2
		job.Processed = 0
		job.Found = 0
		job.Skipped = 0
		job.Errors = 0
		job.Results = []browser.SearchResult{} // Clear stage 1 results
		job.mu.Unlock()
		r.runForewarnStage(job, rows, headers)
	}
}

func (r *Runner) runComptrollerStage(job *Job, rows []map[string]string, headers []string) {
	resultsCh := make(chan comptroller.SearchResult, 10)

	go r.comptrollerClient.SearchAll(rows, resultsCh, job.cancelCh)

	cancelled := false
	var comptrollerResults []comptroller.SearchResult

	for result := range resultsCh {
		// Check if cancelled
		select {
		case <-job.cancelCh:
			cancelled = true
		default:
		}

		job.mu.Lock()
		job.Processed++
		comptrollerResults = append(comptrollerResults, result)
		if result.Error != "" {
			job.Errors++
		} else if result.Skipped {
			job.Skipped++
		} else if result.Found {
			job.Found++
		}
		job.mu.Unlock()

		r.broadcast(job.progress())

		if cancelled {
			break
		}
	}

	// Drain any remaining results if cancelled
	if cancelled {
		for range resultsCh {
			// Just drain the channel
		}
	}

	// Convert comptroller results to browser.SearchResult format for writeResults
	for _, cr := range comptrollerResults {
		br := browser.SearchResult{
			InputRow:   cr.InputRow,
			Found:      cr.Found,
			Skipped:    cr.Skipped,
			SkipReason: cr.SkipReason,
			Output:     cr.Output,
			Error:      cr.Error,
			Timestamp:  cr.Timestamp,
		}

		// Merge comptroller output into input row for stage 2
		if cr.Found {
			cr.InputRow["First Name"] = cr.Output["First Name"]
			cr.InputRow["Last Name"] = cr.Output["Last Name"]
			cr.InputRow["Business Address"] = cr.Output["Business Address"]
		}

		job.mu.Lock()
		job.Results = append(job.Results, br)
		job.mu.Unlock()
	}

	// Write results to CSV (partial if cancelled).
	// For preview/full modes, write to a stage-specific file
	stagePath := job.OutputPath
	if job.Mode == JobModePreview || job.Mode == JobModeFull {
		// Use stage-1 suffix for comptroller results
		stagePath = strings.Replace(job.OutputPath, "-results.csv", "-stage1-results.csv", 1)
	}

	if err := writeResults(stagePath, job.Results, headers); err != nil {
		log.Printf("[runner] [Stage 1] Failed to write results: %v", err)
		job.mu.Lock()
		job.Status = StatusFailed
		job.mu.Unlock()
		r.broadcast(job.progress())
		return
	}

	// For preview mode or cancelled, mark as complete
	if cancelled || job.Mode == JobModePreview {
		job.mu.Lock()
		if cancelled {
			job.Status = StatusCancelled
			log.Printf("[runner] [Stage 1] Job %s cancelled: %d processed (partial results saved)", job.ID, job.Processed)
		} else {
			job.Status = StatusComplete
			log.Printf("[runner] [Stage 1] Preview mode complete: %d processed", job.Processed)
		}
		job.CompletedAt = time.Now()
		job.mu.Unlock()
		r.broadcast(job.progress())
	}

	log.Printf("[runner] [Stage 1] Comptroller stage finished: %d processed, %d found, %d skipped, %d errors",
		job.Processed, job.Found, job.Skipped, job.Errors)
}

func (r *Runner) runForewarnStage(job *Job, rows []map[string]string, headers []string) {
	resultsCh := make(chan browser.SearchResult, 10)

	go r.forewarnSearcher.SearchAll(rows, resultsCh, job.cancelCh)

	cancelled := false
	for result := range resultsCh {
		// Check if cancelled
		select {
		case <-job.cancelCh:
			cancelled = true
		default:
		}

		job.mu.Lock()
		job.Processed++
		job.Results = append(job.Results, result)
		if result.Error != "" {
			job.Errors++
		} else if result.Skipped {
			job.Skipped++
		} else if result.Found {
			job.Found++
		}
		job.mu.Unlock()

		r.broadcast(job.progress())

		if cancelled {
			break
		}
	}

	// Drain any remaining results if cancelled
	if cancelled {
		for range resultsCh {
			// Just drain the channel
		}
	}

	// Write results to CSV (partial if cancelled).
	// For full mode, write to stage-2 file; otherwise use default output path
	stagePath := job.OutputPath
	if job.Mode == JobModeFull {
		stagePath = strings.Replace(job.OutputPath, "-results.csv", "-stage2-results.csv", 1)
	}

	if err := writeResults(stagePath, job.Results, headers); err != nil {
		log.Printf("[runner] [Stage 2] Failed to write results: %v", err)
		job.mu.Lock()
		job.Status = StatusFailed
		job.mu.Unlock()
		r.broadcast(job.progress())
		return
	}

	job.mu.Lock()
	if cancelled {
		job.Status = StatusCancelled
		log.Printf("[runner] [Stage 2] Job %s cancelled: %d processed (partial results saved)", job.ID, job.Processed)
	} else {
		job.Status = StatusComplete
		log.Printf("[runner] [Stage 2] Job complete: %d processed", job.Processed)
	}
	job.CompletedAt = time.Now()
	job.mu.Unlock()

	r.broadcast(job.progress())
	log.Printf("[runner] [Stage 2] Forewarn stage finished: %d processed, %d found, %d skipped, %d errors",
		job.Processed, job.Found, job.Skipped, job.Errors)
}

// GetJob returns the progress for a specific job.
func (r *Runner) GetJob(id string) (Progress, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	job, ok := r.jobs[id]
	if !ok {
		return Progress{}, false
	}
	return job.progress(), true
}

// readCSV reads a CSV file into a slice of maps keyed by header name.
func readCSV(path string) ([]map[string]string, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(records) < 2 {
		return nil, nil, fmt.Errorf("CSV must have a header row and at least one data row")
	}

	headers := records[0]
	var rows []map[string]string
	for _, rec := range records[1:] {
		row := make(map[string]string)
		for i, h := range headers {
			if i < len(rec) {
				row[h] = rec[i]
			}
		}
		rows = append(rows, row)
	}
	return rows, headers, nil
}

// writeResults writes search results to a CSV file.
func writeResults(path string, results []browser.SearchResult, originalHeaders []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Build output headers: original + search result columns.
	outHeaders := make([]string, len(originalHeaders))
	copy(outHeaders, originalHeaders)
	outHeaders = append(outHeaders,
		"status",
		"skip_reason",
		"full_name",
		"age",
		"current_address",
		"mobile_phone",
		"mobile_last_seen",
		"residential_phone",
		"residential_last_seen",
		"property_count",
		"last_purchase_date",
		"error",
	)

	if err := w.Write(outHeaders); err != nil {
		return err
	}

	for _, r := range results {
		row := make([]string, 0, len(outHeaders))
		for _, h := range originalHeaders {
			row = append(row, r.InputRow[h])
		}
		row = append(row,
			r.Output["status"],
			r.SkipReason,
			r.Output["full_name"],
			r.Output["age"],
			r.Output["current_address"],
			r.Output["mobile_phone"],
			r.Output["mobile_last_seen"],
			r.Output["residential_phone"],
			r.Output["residential_last_seen"],
			r.Output["property_count"],
			r.Output["last_purchase_date"],
			r.Error,
		)
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}
