package main

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

type GiteaRun struct {
	RepoID            int            `json:"repoId"`
	Link              string         `json:"link"`
	ViewLink          string         `json:"viewLink"`
	WorkflowID        string         `json:"workflowID"`
	WorkflowLink      string         `json:"workflowLink"`
	Title             string         `json:"title"`
	TitleHTML         string         `json:"titleHTML"`
	Status            string         `json:"status"`
	CanCancel         bool           `json:"canCancel"`
	CanApprove        bool           `json:"canApprove"`
	CanRerun          bool           `json:"canRerun"`
	CanRerunFailed    bool           `json:"canRerunFailed"`
	CanDeleteArtifact bool           `json:"canDeleteArtifact"`
	Done              bool           `json:"done"`
	Duration          string         `json:"duration"`
	TriggeredAt       int64          `json:"triggeredAt"`
	TriggerEvent      string         `json:"triggerEvent"`
	IsSchedule        bool           `json:"isSchedule"`
	RunAttempt        int            `json:"runAttempt"`
	Jobs              []GiteaJob     `json:"jobs"`
	Commit            GiteaCommit    `json:"commit"`
	Attempts          []GiteaAttempt `json:"attempts"`
}

type GiteaAttempt struct {
	Attempt         int    `json:"attempt"`
	Status          string `json:"status"`
	Done            bool   `json:"done"`
	Link            string `json:"link"`
	Current         bool   `json:"current"`
	Latest          bool   `json:"latest"`
	TriggeredAt     int64  `json:"triggeredAt"`
	TriggerUserName string `json:"triggerUserName"`
	TriggerUserLink string `json:"triggerUserLink"`
}

type GiteaJob struct {
	ID               int      `json:"id"`
	Link             string   `json:"link"`
	JobID            string   `json:"jobId"`
	Name             string   `json:"name"`
	Status           string   `json:"status"`
	Duration         string   `json:"duration"`
	Needs            []string `json:"needs,omitempty"`
	ParentJobID      int      `json:"parentJobID"`
	CanRerun         bool     `json:"canRerun"`
	IsReusableCaller bool     `json:"isReusableCaller"`
}

type GiteaCommit struct {
	LocaleCommit   string `json:"localeCommit"`
	LocalePushedBy string `json:"localePushedBy"`
	ShortSHA       string `json:"shortSHA"`
	Link           string `json:"link"`
	Pusher         struct {
		DisplayName string `json:"displayName"`
		Link        string `json:"link"`
	} `json:"pusher"`
	Branch struct {
		Name      string `json:"name"`
		Link      string `json:"link"`
		IsDeleted bool   `json:"isDeleted"`
	} `json:"branch"`
}

type GiteaStep struct {
	Summary  string `json:"summary"`
	Duration string `json:"duration"`
	Status   string `json:"status"`
}

type GiteaCurrentJob struct {
	Title  string      `json:"title"`
	Detail string      `json:"detail"`
	Steps  []GiteaStep `json:"steps"`
}

type GiteaLogLine struct {
	Index     int    `json:"index"`
	Timestamp int64  `json:"timestamp"`
	Message   string `json:"message"`
}

type GiteaStepLog struct {
	Step    int            `json:"step"`
	Cursor  int            `json:"cursor"`
	Started int64          `json:"started"`
	Lines   []GiteaLogLine `json:"lines"`
}

type LogCursor struct {
	Step     int  `json:"step"`
	Cursor   any  `json:"cursor"`
	Expanded bool `json:"expanded"`
}

func giteaStatus(status string) string {
	switch status {
	case "success", "failure", "cancelled", "skipped", "blocked", "waiting", "running":
		return status
	case "failed":
		return "failure"
	case "":
		return "running"
	default:
		return "unknown"
	}
}

func formatDuration(started, finished time.Time) string {
	if started.IsZero() {
		return ""
	}
	if finished.IsZero() {
		finished = time.Now().UTC()
	}
	duration := finished.Sub(started)
	if duration < time.Second {
		return "<1s"
	}
	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm %02ds", int(duration.Minutes()), int(duration.Seconds())%60)
	}
	return fmt.Sprintf("%dh %02dm", int(duration.Hours()), int(duration.Minutes())%60)
}

func formatStatusDuration(started, finished time.Time, status string) string {
	if !doneStatus(status) {
		return formatDuration(started, time.Time{})
	}
	return formatDuration(started, finished)
}

func doneStatus(status string) bool {
	switch giteaStatus(status) {
	case "success", "failure", "cancelled", "skipped":
		return true
	default:
		return false
	}
}

func deriveRunStatus(run Run) string {
	status := giteaStatus(run.Status)
	if status != "running" || len(run.Jobs) == 0 {
		return status
	}

	for _, job := range run.Jobs {
		switch giteaStatus(job.Status) {
		case "failure":
			return "failure"
		case "cancelled":
			return "cancelled"
		case "running", "waiting":
			return "running"
		}
	}
	return "success"
}

func loadRun(db *gorm.DB, runID uint) (Run, error) {
	var run Run
	err := db.
		Preload("Jobs", func(tx *gorm.DB) *gorm.DB { return tx.Order("id asc") }).
		Preload("Jobs.Steps", func(tx *gorm.DB) *gorm.DB { return tx.Order("id asc") }).
		Preload("Jobs.Steps.Logs", func(tx *gorm.DB) *gorm.DB { return tx.Order("id asc") }).
		First(&run, runID).Error
	return run, err
}

func buildGiteaRun(run Run, activeJobID string) (GiteaRun, *Job) {
	jobs := make([]GiteaJob, 0, len(run.Jobs))
	var activeJob *Job
	baseLink := fmt.Sprintf("/runs/%d", run.ID)
	apiBaseLink := fmt.Sprintf("/api/runs/%d", run.ID)
	workflowID := run.Workflow
	if workflowID == "" {
		workflowID = "local act workflow"
	}

	for i := range run.Jobs {
		j := &run.Jobs[i]
		routeJobID := strconv.FormatUint(uint64(j.ID), 10)
		workflowJobID := j.Name
		if workflowJobID == "" {
			workflowJobID = routeJobID
		}
		jobs = append(jobs, GiteaJob{
			ID:               int(j.ID),
			Link:             fmt.Sprintf("%s/jobs/%d", baseLink, j.ID),
			JobID:            workflowJobID,
			Name:             j.Name,
			Status:           giteaStatus(j.Status),
			Duration:         formatStatusDuration(j.CreatedAt, j.UpdatedAt, j.Status),
			Needs:            decodeNeeds(j.Needs),
			ParentJobID:      0,
			CanRerun:         false,
			IsReusableCaller: false,
		})
		if activeJobID != "" && (activeJobID == routeJobID || activeJobID == workflowJobID) {
			activeJob = j
		}
	}

	if activeJob == nil && activeJobID == "" && len(run.Jobs) > 0 {
		activeJob = &run.Jobs[0]
	}

	status := deriveRunStatus(run)
	giteaRun := GiteaRun{
		RepoID:            1,
		Link:              apiBaseLink,
		ViewLink:          baseLink,
		Title:             run.Name,
		TitleHTML:         html.EscapeString(run.Name),
		WorkflowID:        workflowID,
		WorkflowLink:      "",
		Status:            status,
		CanCancel:         status == "running",
		CanApprove:        false,
		CanRerun:          false,
		CanRerunFailed:    false,
		CanDeleteArtifact: false,
		Done:              doneStatus(status),
		Duration:          formatStatusDuration(run.CreatedAt, run.UpdatedAt, status),
		TriggeredAt:       run.CreatedAt.Unix(),
		TriggerEvent:      run.Event,
		IsSchedule:        false,
		RunAttempt:        1,
		Jobs:              jobs,
		Attempts: []GiteaAttempt{{
			Attempt:         1,
			Status:          status,
			Done:            doneStatus(status),
			Link:            baseLink,
			Current:         true,
			Latest:          true,
			TriggeredAt:     run.CreatedAt.Unix(),
			TriggerUserName: "act",
			TriggerUserLink: "",
		}},
	}
	giteaRun.Commit.ShortSHA = run.CommitSHA
	giteaRun.Commit.Link = ""
	giteaRun.Commit.LocaleCommit = "commit"
	giteaRun.Commit.LocalePushedBy = "pushed by"
	giteaRun.Commit.Pusher.DisplayName = "act"
	giteaRun.Commit.Pusher.Link = ""
	giteaRun.Commit.Branch.Name = run.Branch
	giteaRun.Commit.Branch.Link = ""
	giteaRun.Commit.Branch.IsDeleted = false
	return giteaRun, activeJob
}

func RegisterAPI(db *gorm.DB) {
	RegisterAPIWithMux(http.DefaultServeMux, db)
}

func setAPIHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func deleteRunHistory(db *gorm.DB, runID uint) (bool, error) {
	deleted := false
	err := db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&Run{}).Where("id = ?", runID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return nil
		}

		var jobIDs []uint
		if err := tx.Model(&Job{}).Where("run_id = ?", runID).Pluck("id", &jobIDs).Error; err != nil {
			return err
		}

		if len(jobIDs) > 0 {
			var stepIDs []uint
			if err := tx.Model(&Step{}).Where("job_id IN ?", jobIDs).Pluck("id", &stepIDs).Error; err != nil {
				return err
			}
			if len(stepIDs) > 0 {
				if err := tx.Unscoped().Where("step_id IN ?", stepIDs).Delete(&LogLine{}).Error; err != nil {
					return err
				}
			}
			if err := tx.Unscoped().Where("job_id IN ?", jobIDs).Delete(&Step{}).Error; err != nil {
				return err
			}
			if err := tx.Unscoped().Where("run_id = ?", runID).Delete(&Job{}).Error; err != nil {
				return err
			}
		}

		if err := tx.Unscoped().Where("id = ?", runID).Delete(&Run{}).Error; err != nil {
			return err
		}
		deleted = true
		return nil
	})
	return deleted, err
}

func deleteAllRunHistory(db *gorm.DB) (int64, error) {
	var deletedRuns int64
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Run{}).Count(&deletedRuns).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("id > ?", 0).Delete(&LogLine{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("id > ?", 0).Delete(&Step{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("id > ?", 0).Delete(&Job{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("id > ?", 0).Delete(&Run{}).Error; err != nil {
			return err
		}
		return nil
	})
	return deletedRuns, err
}

func RegisterAPIWithMux(mux *http.ServeMux, db *gorm.DB) {
	mux.HandleFunc("/api/runs", func(w http.ResponseWriter, r *http.Request) {
		setAPIHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodDelete {
			deleted, err := deleteAllRunHistory(db)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			broadcast([]byte(`{"event":"runs_deleted"}`))
			json.NewEncoder(w).Encode(map[string]interface{}{"deleted": deleted})
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var runs []Run
		db.Order("id desc").Preload("Jobs").Limit(100).Find(&runs)
		json.NewEncoder(w).Encode(runs)
	})

	mux.HandleFunc("/api/runs/", func(w http.ResponseWriter, r *http.Request) {
		setAPIHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/runs/"), "/")
		parts := strings.Split(path, "/")
		runID64, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			http.Error(w, "Invalid run id", http.StatusBadRequest)
			return
		}

		if r.Method == http.MethodDelete {
			if len(parts) != 1 {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			deleted, err := deleteRunHistory(db, uint(runID64))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !deleted {
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}
			broadcast([]byte(`{"event":"run_deleted"}`))
			json.NewEncoder(w).Encode(map[string]interface{}{"deleted": runID64})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		jobIDStr := ""
		if len(parts) >= 3 && parts[1] == "jobs" {
			jobIDStr = parts[2]
		}

		run, err := loadRun(db, uint(runID64))
		if err != nil {
			http.Error(w, "Not found", 404)
			return
		}

		giteaRun, activeJob := buildGiteaRun(run, jobIDStr)

		reqData := struct {
			LogCursors []LogCursor `json:"logCursors"`
		}{}
		json.NewDecoder(r.Body).Decode(&reqData)

		currentJob := GiteaCurrentJob{Steps: []GiteaStep{}}
		stepsLog := []GiteaStepLog{}

		if activeJob != nil {
			currentJob.Title = activeJob.Name
			currentJob.Detail = activeJob.Status
			for idx, step := range activeJob.Steps {
				currentJob.Steps = append(currentJob.Steps, GiteaStep{
					Summary:  step.Name,
					Duration: formatStatusDuration(step.CreatedAt, step.UpdatedAt, step.Status),
					Status:   step.Status,
				})

				var cursor int
				for _, lc := range reqData.LogCursors {
					if lc.Step == idx {
						if c, ok := lc.Cursor.(float64); ok {
							cursor = int(c)
						}
						break
					}
				}

				logLines := []GiteaLogLine{}
				for i := cursor; i < len(step.Logs); i++ {
					log := step.Logs[i]
					logLines = append(logLines, GiteaLogLine{
						Index:     i + 1,
						Timestamp: log.CreatedAt.Unix(),
						Message:   log.Message,
					})
				}
				stepsLog = append(stepsLog, GiteaStepLog{
					Step:    idx,
					Cursor:  len(step.Logs),
					Started: step.CreatedAt.Unix(),
					Lines:   logLines,
				})
			}
		}

		response := map[string]interface{}{
			"state": map[string]interface{}{
				"run":        giteaRun,
				"currentJob": currentJob,
			},
			"artifacts": []interface{}{},
			"logs": map[string]interface{}{
				"stepsLog": stepsLog,
			},
		}

		json.NewEncoder(w).Encode(response)
	})
}
