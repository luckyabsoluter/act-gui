package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestPipeToDaemonForwardsRawChunks(t *testing.T) {
	input := []byte(strings.Repeat("x", logChunkSize*2+17))
	var original bytes.Buffer
	server, chunks := newChunkCaptureServer(t)
	defer server.Close()

	if err := pipeToDaemon(bytes.NewReader(input), &original, server.Client(), server.URL, 7, "stdout"); err != nil {
		t.Fatalf("pipeToDaemon returned error: %v", err)
	}

	if !bytes.Equal(original.Bytes(), input) {
		t.Fatalf("original output length = %d, want %d", original.Len(), len(input))
	}
	got := chunks.bytes()
	if !bytes.Equal(got, input) {
		t.Fatalf("forwarded chunk bytes length = %d, want %d", len(got), len(input))
	}
	if chunks.count() < 2 {
		t.Fatalf("forwarded chunk count = %d, want multiple chunks", chunks.count())
	}
}

func TestParseLogChunkReassemblesSplitLines(t *testing.T) {
	resetParserTestState()
	db := newTestDB(t)
	run := Run{Name: "act push", Workflow: "src/testdata/workflows/test.yml", Event: "push", Status: "running"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	job := Job{RunID: run.ID, JobID: "build", Name: "Build Artifacts", Status: "waiting"}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}

	logs := []byte("[Build Artifacts] ⭐ Run compile\n[Build Artifacts] ✅  Success - compile\n")
	split := bytes.Index(logs, []byte("⭐")) + 1
	ParseLogChunk(db, run.ID, "stdout", logs[:split])
	ParseLogChunk(db, run.ID, "stdout", logs[split:])

	var steps []Step
	db.Where("job_id = ?", job.ID).Find(&steps)
	if len(steps) != 1 || steps[0].Name != "compile" || steps[0].Status != "success" {
		t.Fatalf("steps = %#v, want one successful compile step", steps)
	}
}

func TestFlushLogChunksForwardsFinalPartialLine(t *testing.T) {
	resetParserTestState()
	db := newTestDB(t)
	run := Run{Name: "act push", Workflow: "src/testdata/workflows/test.yml", Event: "push", Status: "running"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	job := Job{RunID: run.ID, JobID: "build", Name: "build", Status: "waiting"}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}

	ParseLogChunk(db, run.ID, "stdout", []byte("[build] 🏁  Job succeeded"))
	FlushLogChunks(db, run.ID)

	var got Job
	db.First(&got, job.ID)
	if got.Status != "success" {
		t.Fatalf("job status = %q, want success", got.Status)
	}
}

type capturedChunks struct {
	mu     sync.Mutex
	chunks [][]byte
}

func (c *capturedChunks) append(chunk []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chunks = append(c.chunks, append([]byte(nil), chunk...))
}

func (c *capturedChunks) bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return bytes.Join(c.chunks, nil)
}

func (c *capturedChunks) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.chunks)
}

func newChunkCaptureServer(t *testing.T) (*httptest.Server, *capturedChunks) {
	t.Helper()

	chunks := &capturedChunks{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/log" {
			http.NotFound(w, r)
			return
		}

		var payload LogPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		chunk, err := payload.chunk()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if payload.Stream != "stdout" {
			t.Errorf("payload stream = %q, want stdout", payload.Stream)
		}
		chunks.append(chunk)
	}))
	return server, chunks
}
