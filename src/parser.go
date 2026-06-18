package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"gorm.io/gorm"
)

var (
	activeJobs      = make(map[string]uint)
	activeSteps     = make(map[uint]uint)
	logChunkBuffers = make(map[string][]byte)
	parserMu        sync.Mutex
)

func activeJobKey(runID uint, token string) string {
	return fmt.Sprintf("%d-%s", runID, strings.TrimSpace(token))
}

func activeJobID(db *gorm.DB, runID uint, token string) (uint, bool) {
	key := activeJobKey(runID, token)
	if id, exists := activeJobs[key]; exists {
		var count int64
		db.Model(&Job{}).Where("id = ? AND run_id = ?", id, runID).Count(&count)
		if count > 0 {
			return id, true
		}
		delete(activeJobs, key)
	}
	return 0, false
}

func registerActiveJob(runID uint, job Job) {
	for _, token := range []string{job.JobID, job.Name} {
		if strings.TrimSpace(token) != "" {
			activeJobs[activeJobKey(runID, token)] = job.ID
		}
	}
}

func refreshRunStatus(db *gorm.DB, runID uint) {
	if runHasTerminalStatus(db, runID) {
		return
	}

	var jobs []Job
	db.Where("run_id = ?", runID).Find(&jobs)
	if len(jobs) == 0 {
		return
	}

	hasRunning := false
	for _, job := range jobs {
		switch job.Status {
		case "failure":
			db.Model(&Run{}).Where("id = ?", runID).Update("status", "failure")
			return
		case "cancelled":
			db.Model(&Run{}).Where("id = ?", runID).Update("status", "cancelled")
			return
		case "running", "waiting", "":
			hasRunning = true
		}
	}
	if hasRunning {
		db.Model(&Run{}).Where("id = ?", runID).Update("status", "running")
		return
	}
	db.Model(&Run{}).Where("id = ?", runID).Update("status", "success")
}

func ParseLogLine(db *gorm.DB, runID uint, line string) {
	parserMu.Lock()
	defer parserMu.Unlock()

	parseLogLineLocked(db, runID, line)
}

func ParseLogChunk(db *gorm.DB, runID uint, stream string, chunk []byte) {
	parserMu.Lock()
	defer parserMu.Unlock()

	key := logChunkKey(runID, stream)
	data := append(logChunkBuffers[key], chunk...)
	for {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}

		line := data[:idx]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		parseLogLineLocked(db, runID, string(bytes.ToValidUTF8(line, []byte("\uFFFD"))))
		data = data[idx+1:]
	}

	if len(data) == 0 {
		delete(logChunkBuffers, key)
		return
	}
	logChunkBuffers[key] = append([]byte(nil), data...)
}

func FlushLogChunks(db *gorm.DB, runID uint) {
	parserMu.Lock()
	defer parserMu.Unlock()

	prefix := fmt.Sprintf("%d-", runID)
	for key, data := range logChunkBuffers {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if len(data) > 0 {
			if data[len(data)-1] == '\r' {
				data = data[:len(data)-1]
			}
			parseLogLineLocked(db, runID, string(bytes.ToValidUTF8(data, []byte("\uFFFD"))))
		}
		delete(logChunkBuffers, key)
	}
}

func logChunkKey(runID uint, stream string) string {
	if stream == "" {
		stream = "stdout"
	}
	return fmt.Sprintf("%d-%s", runID, stream)
}

func parseLogLineLocked(db *gorm.DB, runID uint, line string) {
	re := regexp.MustCompile(`^\[(?:([^/]+)/)?([^\]]+)\]\s*(.*)`)
	matches := re.FindStringSubmatch(line)

	if len(matches) < 4 {
		return
	}

	jobName := strings.TrimSpace(matches[2])
	if jobName == "" {
		return
	}
	message := matches[3]

	if runHasTerminalStatus(db, runID) {
		if jobID, exists := activeJobID(db, runID, jobName); exists {
			if stepID, hasStep := activeSteps[jobID]; hasStep {
				db.Create(&LogLine{StepID: stepID, Message: message})
			}
		}
		return
	}

	var jobID uint
	if id, exists := activeJobID(db, runID, jobName); exists {
		jobID = id
	} else {
		job := Job{}
		if err := db.Where("run_id = ? AND (job_id = ? OR name = ?)", runID, jobName, jobName).First(&job).Error; err != nil {
			job = Job{RunID: runID, JobID: jobName, Name: jobName}
			db.Create(&job)
		}
		jobID = job.ID
		registerActiveJob(runID, job)
		activeJobs[activeJobKey(runID, jobName)] = jobID
	}
	db.Model(&Job{}).Where("id = ? AND status IN ?", jobID, []string{"waiting", ""}).Update("status", "running")

	stepID, hasStep := activeSteps[jobID]

	if strings.HasPrefix(message, "⭐ Run ") {
		stepName := strings.TrimPrefix(message, "⭐ Run ")
		step := Step{JobID: jobID, Name: stepName}
		db.Create(&step)
		stepID = step.ID
		activeSteps[jobID] = stepID
		hasStep = true
	} else if strings.HasPrefix(message, "✅  Success - ") {
		if hasStep {
			db.Model(&Step{}).Where("id = ?", stepID).Update("status", "success")
		} else {
			db.Model(&Job{}).Where("id = ?", jobID).Update("status", "success")
		}
	} else if strings.HasPrefix(message, "❌  Failure - ") {
		if hasStep {
			db.Model(&Step{}).Where("id = ?", stepID).Update("status", "failure")
		} else {
			db.Model(&Job{}).Where("id = ?", jobID).Update("status", "failure")
		}
	} else if message == "🏁  Job succeeded" {
		db.Model(&Job{}).Where("id = ?", jobID).Update("status", "success")
	} else if message == "🏁  Job failed" {
		db.Model(&Job{}).Where("id = ?", jobID).Update("status", "failure")
	}

	if hasStep {
		logLine := LogLine{StepID: stepID, Message: message}
		db.Create(&logLine)
	}
	refreshRunStatus(db, runID)
}
