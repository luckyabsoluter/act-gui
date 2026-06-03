package main

import (
	"path/filepath"
	"testing"
)

func TestInferActEvent(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "default", args: nil, want: "push"},
		{name: "positional event", args: []string{"pull_request"}, want: "pull_request"},
		{name: "job flag only", args: []string{"-j", "build"}, want: "push"},
		{name: "long job flag before event", args: []string{"--job", "build", "workflow_dispatch"}, want: "workflow_dispatch"},
		{name: "long job flag with value before event", args: []string{"--job=build", "pull_request"}, want: "pull_request"},
		{name: "workflow flag before event", args: []string{"-W", ".github/workflows/test.yml", "workflow_dispatch"}, want: "workflow_dispatch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inferActEvent(tt.args); got != tt.want {
				t.Fatalf("inferActEvent(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestBuildStartRunPayloadPreservesWorkflowFlag(t *testing.T) {
	t.Chdir("..")

	payload := buildStartRunPayload([]string{"-W", ".github/workflows/test.yml", "-j", "lint", "workflow_dispatch"})
	if payload.Workflow != ".github/workflows/test.yml" {
		t.Fatalf("Workflow = %q, want .github/workflows/test.yml", payload.Workflow)
	}
	if payload.Event != "workflow_dispatch" {
		t.Fatalf("Event = %q, want workflow_dispatch", payload.Event)
	}
	if payload.Name != "act workflow_dispatch / lint" {
		t.Fatalf("Name = %q, want act workflow_dispatch / lint", payload.Name)
	}
	if len(payload.Jobs) != 5 {
		t.Fatalf("Jobs length = %d, want 5", len(payload.Jobs))
	}
	if payload.Jobs[0].Name != "lint" {
		t.Fatalf("Jobs[0].Name = %q, want lint", payload.Jobs[0].Name)
	}
	if payload.Jobs[1].Name != "build" || len(payload.Jobs[1].Needs) != 1 || payload.Jobs[1].Needs[0] != "lint" {
		t.Fatalf("Jobs[1] = %#v, want build needing lint", payload.Jobs[1])
	}
}

func TestActGUIDataDirUsesPlatformDataDirectories(t *testing.T) {
	env := map[string]string{
		"APPDATA":       filepath.Join("C:", "Users", "tester", "AppData", "Roaming"),
		"LOCALAPPDATA":  filepath.Join("C:", "Users", "tester", "AppData", "Local"),
		"XDG_DATA_HOME": filepath.Join("home", "tester", ".local", "share"),
	}

	tests := []struct {
		name string
		goos string
		env  map[string]string
		home string
		want string
	}{
		{
			name: "windows appdata",
			goos: "windows",
			env:  env,
			home: filepath.Join("C:", "Users", "tester"),
			want: filepath.Join(env["APPDATA"], "act-gui"),
		},
		{
			name: "windows local appdata fallback",
			goos: "windows",
			env: map[string]string{
				"LOCALAPPDATA": env["LOCALAPPDATA"],
			},
			home: filepath.Join("C:", "Users", "tester"),
			want: filepath.Join(env["LOCALAPPDATA"], "act-gui"),
		},
		{
			name: "darwin application support",
			goos: "darwin",
			env:  nil,
			home: filepath.Join("Users", "tester"),
			want: filepath.Join("Users", "tester", "Library", "Application Support", "act-gui"),
		},
		{
			name: "linux xdg data home",
			goos: "linux",
			env:  env,
			home: filepath.Join("home", "tester"),
			want: filepath.Join(env["XDG_DATA_HOME"], "act-gui"),
		},
		{
			name: "linux local share fallback",
			goos: "linux",
			env:  nil,
			home: filepath.Join("home", "tester"),
			want: filepath.Join("home", "tester", ".local", "share", "act-gui"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := actGUIDataDirFor(tt.goos, func(key string) string {
				if tt.env == nil {
					return ""
				}
				return tt.env[key]
			}, tt.home)
			if err != nil {
				t.Fatalf("actGUIDataDirFor returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("actGUIDataDirFor = %q, want %q", got, tt.want)
			}
		})
	}
}
