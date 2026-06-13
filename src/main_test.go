package main

import (
	"path/filepath"
	"strings"
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
		{name: "workflow flag before event", args: []string{"-W", "src/testdata/workflows/test.yml", "workflow_dispatch"}, want: "workflow_dispatch"},
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

	payload := buildStartRunPayload([]string{"-W", "src/testdata/workflows/test.yml", "-j", "lint", "workflow_dispatch"})
	if payload.Workflow != "src/testdata/workflows/test.yml" {
		t.Fatalf("Workflow = %q, want src/testdata/workflows/test.yml", payload.Workflow)
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
	if payload.Jobs[0].JobID != "lint" || payload.Jobs[0].Name != "Lint Code" {
		t.Fatalf("Jobs[0] = %#v, want lint job ID and Lint Code name", payload.Jobs[0])
	}
	if payload.Jobs[1].JobID != "build" || payload.Jobs[1].Name != "Build Artifacts" || len(payload.Jobs[1].Needs) != 1 || payload.Jobs[1].Needs[0] != "lint" {
		t.Fatalf("Jobs[1] = %#v, want build job ID with Build Artifacts name needing lint", payload.Jobs[1])
	}
}

func TestParseActGUIArgsUsesDefaultPort(t *testing.T) {
	port, actArgs, err := parseActGUIArgs([]string{"-W", "src/testdata/workflows/test.yml"})
	if err != nil {
		t.Fatalf("parseActGUIArgs returned error: %v", err)
	}
	if port != "27979" {
		t.Fatalf("port = %q, want 27979", port)
	}
	if len(actArgs) != 2 || actArgs[0] != "-W" || actArgs[1] != "src/testdata/workflows/test.yml" {
		t.Fatalf("actArgs = %#v", actArgs)
	}
}

func TestParseActGUIArgsStripsPortFlag(t *testing.T) {
	port, actArgs, err := parseActGUIArgs([]string{"--act-gui-port", "28000", "-W", "src/testdata/workflows/test.yml"})
	if err != nil {
		t.Fatalf("parseActGUIArgs returned error: %v", err)
	}
	if port != "28000" {
		t.Fatalf("port = %q, want 28000", port)
	}
	if len(actArgs) != 2 || actArgs[0] != "-W" || actArgs[1] != "src/testdata/workflows/test.yml" {
		t.Fatalf("actArgs = %#v", actArgs)
	}
}

func TestParseActGUIArgsStripsEqualsPortFlag(t *testing.T) {
	port, actArgs, err := parseActGUIArgs([]string{"--act-gui-port=28000", "workflow_dispatch"})
	if err != nil {
		t.Fatalf("parseActGUIArgs returned error: %v", err)
	}
	if port != "28000" {
		t.Fatalf("port = %q, want 28000", port)
	}
	if len(actArgs) != 1 || actArgs[0] != "workflow_dispatch" {
		t.Fatalf("actArgs = %#v", actArgs)
	}
}

func TestParseActGUIArgsRejectsInvalidPort(t *testing.T) {
	tests := [][]string{
		{"--act-gui-port"},
		{"--act-gui-port=0"},
		{"--act-gui-port=65536"},
		{"--act-gui-port=abc"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, _, err := parseActGUIArgs(args); err == nil {
				t.Fatalf("parseActGUIArgs(%#v) returned nil error", args)
			}
		})
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
