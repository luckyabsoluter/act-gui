package main

import "gorm.io/gorm"

type Run struct {
	gorm.Model
	Name      string
	Workflow  string
	Event     string
	Branch    string
	CommitSHA string
	Status    string `gorm:"default:running"` // running, success, failure, cancelled
	Jobs      []Job
}

type Job struct {
	gorm.Model
	RunID  uint
	Name   string
	Status string `gorm:"default:running"` // running, success, failure, cancelled
	Needs  string
	Steps  []Step
}

type Step struct {
	gorm.Model
	JobID  uint
	Name   string
	Status string `gorm:"default:running"` // running, success, failure, cancelled
	Logs   []LogLine
}

type LogLine struct {
	gorm.Model
	StepID  uint
	Message string
}
