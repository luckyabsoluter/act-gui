package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type actionRunResponse struct {
	State struct {
		Run struct {
			ViewLink   string `json:"viewLink"`
			WorkflowID string `json:"workflowID"`
			Jobs       []struct {
				ID       int      `json:"id"`
				Link     string   `json:"link"`
				JobID    string   `json:"jobId"`
				Name     string   `json:"name"`
				Status   string   `json:"status"`
				Duration string   `json:"duration"`
				Needs    []string `json:"needs"`
			} `json:"jobs"`
		} `json:"run"`
		CurrentJob struct {
			Title string `json:"title"`
			Steps []struct {
				Summary string `json:"summary"`
				Status  string `json:"status"`
			} `json:"steps"`
		} `json:"currentJob"`
	} `json:"state"`
	Logs struct {
		StepsLog []struct {
			Step   int `json:"step"`
			Cursor int `json:"cursor"`
			Lines  []struct {
				Index   int    `json:"index"`
				Message string `json:"message"`
			} `json:"lines"`
		} `json:"stepsLog"`
	} `json:"logs"`
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(&Run{}, &Job{}, &Step{}, &LogLine{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return db
}

func createRunScenario(t *testing.T, db *gorm.DB) (Run, Job, Job) {
	t.Helper()
	run := Run{
		Name:      "act workflow_dispatch",
		Workflow:  "src/testdata/workflows/test.yml",
		Event:     "workflow_dispatch",
		Branch:    "main",
		CommitSHA: "abc1234",
		Status:    "success",
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}

	lint := Job{RunID: run.ID, JobID: "lint", Name: "Lint Code", Status: "success"}
	build := Job{RunID: run.ID, JobID: "build", Name: "Build Artifacts", Status: "success", Needs: "lint"}
	if err := db.Create(&lint).Error; err != nil {
		t.Fatalf("create lint job: %v", err)
	}
	if err := db.Create(&build).Error; err != nil {
		t.Fatalf("create build job: %v", err)
	}

	step := Step{JobID: lint.ID, Name: "Main Run Linter", Status: "success"}
	if err := db.Create(&step).Error; err != nil {
		t.Fatalf("create step: %v", err)
	}
	for _, message := range []string{"first log line", "second log line"} {
		if err := db.Create(&LogLine{StepID: step.ID, Message: message}).Error; err != nil {
			t.Fatalf("create log line: %v", err)
		}
	}
	return run, lint, build
}

func serveTestAPI(db *gorm.DB) *http.ServeMux {
	mux := http.NewServeMux()
	RegisterAPIWithMux(mux, db)
	return mux
}

func decodeActionRun(t *testing.T, mux *http.ServeMux, method, path string, body []byte) actionRunResponse {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s status = %d, body = %s", method, path, rec.Code, rec.Body.String())
	}

	var response actionRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode action run response: %v", err)
	}
	return response
}

func TestActionAPIExposesWorkflowRunsJobsAndCursorLogs(t *testing.T) {
	db := newTestDB(t)
	run, lint, build := createRunScenario(t, db)
	mux := serveTestAPI(db)

	listReq := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("GET /api/runs status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var runs []Run
	if err := json.Unmarshal(listRec.Body.Bytes(), &runs); err != nil {
		t.Fatalf("decode run list: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != run.ID || len(runs[0].Jobs) != 2 {
		t.Fatalf("run list = %#v, want one run with two jobs", runs)
	}

	summary := decodeActionRun(t, mux, http.MethodPost, fmt.Sprintf("/api/runs/%d", run.ID), nil)
	if summary.State.Run.WorkflowID != "src/testdata/workflows/test.yml" {
		t.Fatalf("summary workflowID = %q", summary.State.Run.WorkflowID)
	}
	if summary.State.Run.ViewLink != fmt.Sprintf("/runs/%d", run.ID) {
		t.Fatalf("summary viewLink = %q", summary.State.Run.ViewLink)
	}
	if len(summary.State.Run.Jobs) != 2 {
		t.Fatalf("summary jobs length = %d, want 2", len(summary.State.Run.Jobs))
	}
	if summary.State.Run.Jobs[0].ID != int(lint.ID) || summary.State.Run.Jobs[0].Link != fmt.Sprintf("/runs/%d/jobs/%d", run.ID, lint.ID) {
		t.Fatalf("summary first job = %#v", summary.State.Run.Jobs[0])
	}
	if summary.State.Run.Jobs[0].JobID != "lint" {
		t.Fatalf("summary first job ID = %q, want lint", summary.State.Run.Jobs[0].JobID)
	}
	if summary.State.Run.Jobs[0].Name != "Lint Code" {
		t.Fatalf("summary first job name = %q, want Lint Code", summary.State.Run.Jobs[0].Name)
	}
	if summary.State.Run.Jobs[1].ID != int(build.ID) {
		t.Fatalf("summary second job = %#v", summary.State.Run.Jobs[1])
	}
	if len(summary.State.Run.Jobs[1].Needs) != 1 || summary.State.Run.Jobs[1].Needs[0] != "lint" {
		t.Fatalf("summary second job needs = %#v, want [lint]", summary.State.Run.Jobs[1].Needs)
	}

	body := []byte(`{"logCursors":[{"step":0,"cursor":1,"expanded":true}]}`)
	job := decodeActionRun(t, mux, http.MethodPost, fmt.Sprintf("/api/runs/%d/jobs/%d", run.ID, lint.ID), body)
	if job.State.CurrentJob.Title != "Lint Code" {
		t.Fatalf("current job title = %q, want Lint Code", job.State.CurrentJob.Title)
	}
	if len(job.State.CurrentJob.Steps) != 1 || job.State.CurrentJob.Steps[0].Summary != "Main Run Linter" {
		t.Fatalf("current job steps = %#v", job.State.CurrentJob.Steps)
	}
	if len(job.Logs.StepsLog) != 1 || job.Logs.StepsLog[0].Cursor != 2 {
		t.Fatalf("steps log = %#v", job.Logs.StepsLog)
	}
	if len(job.Logs.StepsLog[0].Lines) != 1 || job.Logs.StepsLog[0].Lines[0].Index != 2 || job.Logs.StepsLog[0].Lines[0].Message != "second log line" {
		t.Fatalf("cursor-filtered lines = %#v", job.Logs.StepsLog[0].Lines)
	}
}

func TestActionAPIDoesNotStartDurationForUnstartedJobs(t *testing.T) {
	db := newTestDB(t)
	run := Run{Name: "act push", Workflow: "src/testdata/workflows/test.yml", Event: "push", Status: "running"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	waiting := Job{RunID: run.ID, JobID: "deploy", Name: "Deploy", Status: "waiting"}
	if err := db.Create(&waiting).Error; err != nil {
		t.Fatalf("create waiting job: %v", err)
	}
	mux := serveTestAPI(db)

	summary := decodeActionRun(t, mux, http.MethodPost, fmt.Sprintf("/api/runs/%d", run.ID), nil)
	if summary.State.Run.Jobs[0].Duration != "" {
		t.Fatalf("waiting job duration = %q, want empty", summary.State.Run.Jobs[0].Duration)
	}

	finishRun(db, run.ID, "success")
	summary = decodeActionRun(t, mux, http.MethodPost, fmt.Sprintf("/api/runs/%d", run.ID), nil)
	if summary.State.Run.Jobs[0].Status != "skipped" {
		t.Fatalf("job status = %q, want skipped", summary.State.Run.Jobs[0].Status)
	}
	if summary.State.Run.Jobs[0].Duration != "" {
		t.Fatalf("skipped job duration = %q, want empty", summary.State.Run.Jobs[0].Duration)
	}
}

func TestActionAPIInfersLegacyJobIDsFromNeeds(t *testing.T) {
	db := newTestDB(t)
	run := Run{Name: "act push", Workflow: ".github/workflows/ci.yml", Event: "push", Status: "cancelled"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	testJob := Job{RunID: run.ID, Name: "Test", Status: "cancelled"}
	releaseJob := Job{RunID: run.ID, Name: "Release", Status: "cancelled", Needs: "test"}
	deployJob := Job{RunID: run.ID, Name: "Deploy", Status: "cancelled", Needs: "release"}
	for _, job := range []*Job{&testJob, &releaseJob, &deployJob} {
		if err := db.Create(job).Error; err != nil {
			t.Fatalf("create job: %v", err)
		}
	}
	mux := serveTestAPI(db)

	summary := decodeActionRun(t, mux, http.MethodPost, fmt.Sprintf("/api/runs/%d", run.ID), nil)
	if summary.State.Run.Jobs[0].JobID != "test" {
		t.Fatalf("legacy upstream job ID = %q, want test", summary.State.Run.Jobs[0].JobID)
	}
	if summary.State.Run.Jobs[1].JobID != "release" {
		t.Fatalf("legacy middle job ID = %q, want release", summary.State.Run.Jobs[1].JobID)
	}
	if len(summary.State.Run.Jobs[1].Needs) != 1 || summary.State.Run.Jobs[1].Needs[0] != "test" {
		t.Fatalf("legacy middle needs = %#v, want [test]", summary.State.Run.Jobs[1].Needs)
	}
	if len(summary.State.Run.Jobs[2].Needs) != 1 || summary.State.Run.Jobs[2].Needs[0] != "release" {
		t.Fatalf("legacy downstream needs = %#v, want [release]", summary.State.Run.Jobs[2].Needs)
	}
	for _, job := range summary.State.Run.Jobs {
		if job.Duration != "" {
			t.Fatalf("legacy unstarted job %q duration = %q, want empty", job.Name, job.Duration)
		}
	}
}

func TestActionAPIFallsBackEmptyWorkflowName(t *testing.T) {
	db := newTestDB(t)
	run := Run{Name: "act push", Event: "push", Status: "success"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	mux := serveTestAPI(db)

	summary := decodeActionRun(t, mux, http.MethodPost, fmt.Sprintf("/api/runs/%d", run.ID), nil)
	if summary.State.Run.WorkflowID != "local act workflow" {
		t.Fatalf("workflowID = %q, want local act workflow", summary.State.Run.WorkflowID)
	}
	if len(summary.State.Run.Jobs) != 0 {
		t.Fatalf("jobs length = %d, want 0", len(summary.State.Run.Jobs))
	}
}

func assertModelCount(t *testing.T, db *gorm.DB, model interface{}, want int64) {
	t.Helper()
	var got int64
	if err := db.Unscoped().Model(model).Count(&got).Error; err != nil {
		t.Fatalf("count model: %v", err)
	}
	if got != want {
		t.Fatalf("model count = %d, want %d", got, want)
	}
}

func TestDeleteRunHistoryRemovesJobsStepsAndLogs(t *testing.T) {
	db := newTestDB(t)
	run, _, _ := createRunScenario(t, db)
	mux := serveTestAPI(db)

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/runs/%d", run.ID), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/runs/%d status = %d, body = %s", run.ID, rec.Code, rec.Body.String())
	}

	assertModelCount(t, db, &Run{}, 0)
	assertModelCount(t, db, &Job{}, 0)
	assertModelCount(t, db, &Step{}, 0)
	assertModelCount(t, db, &LogLine{}, 0)
}

func TestDeleteAllRunHistoryClearsRunsJobsStepsAndLogs(t *testing.T) {
	db := newTestDB(t)
	createRunScenario(t, db)
	createRunScenario(t, db)
	mux := serveTestAPI(db)

	req := httptest.NewRequest(http.MethodDelete, "/api/runs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/runs status = %d, body = %s", rec.Code, rec.Body.String())
	}

	assertModelCount(t, db, &Run{}, 0)
	assertModelCount(t, db, &Job{}, 0)
	assertModelCount(t, db, &Step{}, 0)
	assertModelCount(t, db, &LogLine{}, 0)
}
