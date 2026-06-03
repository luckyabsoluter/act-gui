package main

import "testing"

func TestWorkflowJobsFromContentPreservesJobsAndNeeds(t *testing.T) {
	jobs := workflowJobsFromContent([]byte(`
name: Test
on: [push]
jobs:
  lint:
    runs-on: ubuntu-latest
    steps: []
  build:
    needs: lint
    runs-on: ubuntu-latest
    steps: []
  deploy:
    needs: [build, lint]
    runs-on: ubuntu-latest
    steps: []
`))

	if len(jobs) != 3 {
		t.Fatalf("jobs length = %d, want 3", len(jobs))
	}
	if jobs[0].Name != "lint" {
		t.Fatalf("jobs[0].Name = %q, want lint", jobs[0].Name)
	}
	if jobs[1].Name != "build" || len(jobs[1].Needs) != 1 || jobs[1].Needs[0] != "lint" {
		t.Fatalf("jobs[1] = %#v, want build needing lint", jobs[1])
	}
	if jobs[2].Name != "deploy" || len(jobs[2].Needs) != 2 || jobs[2].Needs[0] != "build" || jobs[2].Needs[1] != "lint" {
		t.Fatalf("jobs[2] = %#v, want deploy needing build and lint", jobs[2])
	}
}
