package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestParseActGUIConfigUsesDefaultHostAndPort(t *testing.T) {
	host, port, actArgs, err := parseActGUIConfig([]string{"-W", "src/testdata/workflows/test.yml"})
	if err != nil {
		t.Fatalf("parseActGUIConfig returned error: %v", err)
	}
	if host != "localhost" {
		t.Fatalf("host = %q, want localhost", host)
	}
	if port != "27979" {
		t.Fatalf("port = %q, want 27979", port)
	}
	if len(actArgs) != 2 || actArgs[0] != "-W" || actArgs[1] != "src/testdata/workflows/test.yml" {
		t.Fatalf("actArgs = %#v", actArgs)
	}
}

func TestParseActGUIConfigStripsHostFlag(t *testing.T) {
	host, port, actArgs, err := parseActGUIConfig([]string{"--act-gui-host", "127.0.0.1", "--act-gui-port", "28000", "-W", "src/testdata/workflows/test.yml"})
	if err != nil {
		t.Fatalf("parseActGUIConfig returned error: %v", err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("host = %q, want 127.0.0.1", host)
	}
	if port != "28000" {
		t.Fatalf("port = %q, want 28000", port)
	}
	if len(actArgs) != 2 || actArgs[0] != "-W" || actArgs[1] != "src/testdata/workflows/test.yml" {
		t.Fatalf("actArgs = %#v", actArgs)
	}
}

func TestParseActGUIConfigStripsEqualsHostFlag(t *testing.T) {
	host, port, actArgs, err := parseActGUIConfig([]string{"--act-gui-host=127.0.0.1", "workflow_dispatch"})
	if err != nil {
		t.Fatalf("parseActGUIConfig returned error: %v", err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("host = %q, want 127.0.0.1", host)
	}
	if port != "27979" {
		t.Fatalf("port = %q, want 27979", port)
	}
	if len(actArgs) != 1 || actArgs[0] != "workflow_dispatch" {
		t.Fatalf("actArgs = %#v", actArgs)
	}
}

func TestParseActGUIConfigRejectsInvalidHost(t *testing.T) {
	tests := [][]string{
		{"--act-gui-host"},
		{"--act-gui-host="},
		{"--act-gui-host", "http://localhost"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, _, _, err := parseActGUIConfig(args); err == nil {
				t.Fatalf("parseActGUIConfig(%#v) returned nil error", args)
			}
		})
	}
}

func TestDaemonBaseURLUsesHostAndPort(t *testing.T) {
	if got := daemonBaseURL("127.0.0.1", "28000"); got != "http://127.0.0.1:28000" {
		t.Fatalf("daemonBaseURL = %q, want http://127.0.0.1:28000", got)
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

func TestActGUIHelpRequested(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "long help", args: []string{"--help"}, want: true},
		{name: "short help", args: []string{"-h"}, want: true},
		{name: "help after act args", args: []string{"-W", "src/testdata/workflows/test.yml", "--help"}, want: true},
		{name: "after separator", args: []string{"--", "--help"}, want: false},
		{name: "act help", args: []string{"--act-help"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := actGUIHelpRequested(tt.args); got != tt.want {
				t.Fatalf("actGUIHelpRequested(%#v) = %t, want %t", tt.args, got, tt.want)
			}
		})
	}
}

func TestActHelpRequested(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "act help", args: []string{"--act-help"}, want: true},
		{name: "act help after act args", args: []string{"-W", "src/testdata/workflows/test.yml", "--act-help"}, want: true},
		{name: "after separator", args: []string{"--", "--act-help"}, want: false},
		{name: "act gui help", args: []string{"--help"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := actHelpRequested(tt.args); got != tt.want {
				t.Fatalf("actHelpRequested(%#v) = %t, want %t", tt.args, got, tt.want)
			}
		})
	}
}

func TestActGUIVersionRequested(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "version", args: []string{"--version"}, want: true},
		{name: "version after act args", args: []string{"-W", "src/testdata/workflows/test.yml", "--version"}, want: true},
		{name: "after separator", args: []string{"--", "--version"}, want: false},
		{name: "act gui help", args: []string{"--help"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := actGUIVersionRequested(tt.args); got != tt.want {
				t.Fatalf("actGUIVersionRequested(%#v) = %t, want %t", tt.args, got, tt.want)
			}
		})
	}
}

func TestPrintActGUIHelp(t *testing.T) {
	var buf bytes.Buffer
	printActGUIHelp(&buf)
	help := buf.String()

	for _, want := range []string{
		"act-gui [act-gui options] [act options] [event]",
		"Any act options and event arguments can be passed after act-gui options.",
		"--act-gui-host <host>",
		"--act-gui-port <port>",
		"--act-help",
		"--version",
		"-h, --help",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("act-gui help does not contain %q:\n%s", want, help)
		}
	}
}

func TestPrintActGUIVersion(t *testing.T) {
	var buf bytes.Buffer
	printActGUIVersion(&buf)
	version := buf.String()
	actVersion := actLibraryVersion()
	if actVersion == "unknown" {
		t.Fatal("actLibraryVersion() = unknown")
	}

	for _, want := range []string{
		"act-gui: " + ActGUIVersion,
		"act library: " + actModulePath + " " + actVersion,
	} {
		if !strings.Contains(version, want) {
			t.Fatalf("act-gui version does not contain %q:\n%s", want, version)
		}
	}
}

func TestProbeDaemonAcceptsCompatibleDaemon(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(currentDaemonInfo())
	}))
	defer server.Close()

	client := &http.Client{Timeout: time.Second}
	info, reachable, err := probeDaemon(client, server.URL)
	if err != nil {
		t.Fatalf("probeDaemon returned error: %v", err)
	}
	if !reachable {
		t.Fatal("probeDaemon reachable = false, want true")
	}
	if info.Protocol != daemonProtocol || info.Version != ActGUIVersion {
		t.Fatalf("daemon info = %#v, want protocol %d version %q", info, daemonProtocol, ActGUIVersion)
	}
}

func TestProbeDaemonRejectsLegacyPong(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	}))
	defer server.Close()

	client := &http.Client{Timeout: time.Second}
	_, reachable, err := probeDaemon(client, server.URL)
	if !reachable {
		t.Fatal("probeDaemon reachable = false, want true")
	}
	if err == nil {
		t.Fatal("probeDaemon returned nil error for legacy pong")
	}
	if !strings.Contains(err.Error(), "unsupported daemon response") {
		t.Fatalf("probeDaemon error = %q, want unsupported daemon response", err.Error())
	}
}

func TestProbeDaemonRejectsProtocolMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := currentDaemonInfo()
		info.Protocol++
		json.NewEncoder(w).Encode(info)
	}))
	defer server.Close()

	client := &http.Client{Timeout: time.Second}
	_, reachable, err := probeDaemon(client, server.URL)
	if !reachable {
		t.Fatal("probeDaemon reachable = false, want true")
	}
	if err == nil {
		t.Fatal("probeDaemon returned nil error for protocol mismatch")
	}
	if !strings.Contains(err.Error(), "does not match required protocol") {
		t.Fatalf("probeDaemon error = %q, want protocol mismatch", err.Error())
	}
}

func TestProbeDaemonRejectsVersionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := currentDaemonInfo()
		info.Version = "act-gui-dev-old"
		json.NewEncoder(w).Encode(info)
	}))
	defer server.Close()

	client := &http.Client{Timeout: time.Second}
	_, reachable, err := probeDaemon(client, server.URL)
	if !reachable {
		t.Fatal("probeDaemon reachable = false, want true")
	}
	if err == nil {
		t.Fatal("probeDaemon returned nil error for version mismatch")
	}
	if !strings.Contains(err.Error(), "does not match client version") {
		t.Fatalf("probeDaemon error = %q, want version mismatch", err.Error())
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
