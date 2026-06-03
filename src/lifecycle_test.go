package main

import (
	"context"
	"testing"
	"time"
)

func TestFinishRunCancelsRunningChildren(t *testing.T) {
	db := newTestDB(t)
	run := Run{Name: "act push", Workflow: ".github/workflows/test.yml", Event: "push", Status: "running"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	job := Job{RunID: run.ID, Name: "build", Status: "running"}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}
	step := Step{JobID: job.ID, Name: "Run tests", Status: "running"}
	if err := db.Create(&step).Error; err != nil {
		t.Fatalf("create step: %v", err)
	}

	status := finishRun(db, run.ID, "cancelled")
	if status != "cancelled" {
		t.Fatalf("finish status = %q, want cancelled", status)
	}

	var gotRun Run
	var gotJob Job
	var gotStep Step
	db.First(&gotRun, run.ID)
	db.First(&gotJob, job.ID)
	db.First(&gotStep, step.ID)
	if gotRun.Status != "cancelled" {
		t.Fatalf("run status = %q, want cancelled", gotRun.Status)
	}
	if gotJob.Status != "cancelled" {
		t.Fatalf("job status = %q, want cancelled", gotJob.Status)
	}
	if gotStep.Status != "cancelled" {
		t.Fatalf("step status = %q, want cancelled", gotStep.Status)
	}
}

func TestRunCompletionStatusMarksCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	status := runCompletionStatus(ctx)
	if status != "cancelled" {
		t.Fatalf("run completion status = %q, want cancelled", status)
	}
}

func TestWatchRunCancellationPostsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan string, 1)
	stop := watchRunCancellation(ctx, func(status string) {
		finished <- status
	})

	cancel()

	select {
	case status := <-finished:
		if status != "cancelled" {
			t.Fatalf("finish status = %q, want cancelled", status)
		}
	case <-time.After(time.Second):
		t.Fatal("watchRunCancellation did not post cancelled status")
	}

	stop()
}

func TestRefreshRunStatusDoesNotReopenCancelledRun(t *testing.T) {
	db := newTestDB(t)
	run := Run{Name: "act push", Workflow: ".github/workflows/test.yml", Event: "push", Status: "cancelled"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := db.Create(&Job{RunID: run.ID, Name: "build", Status: "running"}).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}

	refreshRunStatus(db, run.ID)

	var got Run
	db.First(&got, run.ID)
	if got.Status != "cancelled" {
		t.Fatalf("run status = %q, want cancelled", got.Status)
	}
}

func TestParseLogLineDoesNotCreateRunningWorkAfterCancellation(t *testing.T) {
	db := newTestDB(t)
	run := Run{Name: "act push", Workflow: ".github/workflows/test.yml", Event: "push", Status: "cancelled"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}

	ParseLogLine(db, run.ID, "[build] ⭐ Run tests")

	var jobs []Job
	db.Where("run_id = ?", run.ID).Find(&jobs)
	if len(jobs) != 0 {
		t.Fatalf("jobs length = %d, want 0 after cancelled run log", len(jobs))
	}
}

func TestParseLogLineUpdatesSeededJob(t *testing.T) {
	db := newTestDB(t)
	run := Run{Name: "act push", Workflow: ".github/workflows/test.yml", Event: "push", Status: "running"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	job := Job{RunID: run.ID, Name: "build", Status: "waiting", Needs: "lint"}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}

	ParseLogLine(db, run.ID, "[build] ⭐ Run tests")

	var jobs []Job
	db.Where("run_id = ?", run.ID).Find(&jobs)
	if len(jobs) != 1 {
		t.Fatalf("jobs length = %d, want 1", len(jobs))
	}
	if jobs[0].ID != job.ID {
		t.Fatalf("job ID = %d, want seeded job %d", jobs[0].ID, job.ID)
	}
	if jobs[0].Status != "running" {
		t.Fatalf("job status = %q, want running", jobs[0].Status)
	}
	if jobs[0].Needs != "lint" {
		t.Fatalf("job needs = %q, want lint", jobs[0].Needs)
	}

	var steps []Step
	db.Where("job_id = ?", job.ID).Find(&steps)
	if len(steps) != 1 || steps[0].Name != "tests" {
		t.Fatalf("steps = %#v, want one tests step", steps)
	}
}

func TestFinishRunSkipsWaitingJobsOnSuccess(t *testing.T) {
	db := newTestDB(t)
	run := Run{Name: "act push", Workflow: ".github/workflows/test.yml", Event: "push", Status: "running"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	waiting := Job{RunID: run.ID, Name: "deploy", Status: "waiting"}
	if err := db.Create(&waiting).Error; err != nil {
		t.Fatalf("create waiting job: %v", err)
	}

	status := finishRun(db, run.ID, "success")
	if status != "success" {
		t.Fatalf("finish status = %q, want success", status)
	}

	var got Job
	db.First(&got, waiting.ID)
	if got.Status != "skipped" {
		t.Fatalf("waiting job status = %q, want skipped", got.Status)
	}
}
