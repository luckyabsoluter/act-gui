package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	actcmd "github.com/nektos/act/cmd"
	"gorm.io/gorm"
)

//go:embed ui/dist/*
//go:embed ui/dist/assets/*
var uiFiles embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var (
	clients   = make(map[*websocket.Conn]bool)
	clientsMu sync.Mutex
)

const (
	internalDaemonFlag = "--act-gui-daemon"
	internalRunnerFlag = "--act-gui-runner"
	actGUIPortFlag     = "--act-gui-port"
	defaultDaemonPort  = "27979"
	daemonHost         = "localhost"
	daemonProtocol     = 1
)

var ActGUIVersion = "act-gui dev"

type DaemonInfo struct {
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
	BuildID  string `json:"build_id"`
	PID      int    `json:"pid"`
}

func daemonBaseURL(port string) string {
	return "http://" + daemonHost + ":" + port
}

func daemonBuildID() string {
	exe, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	info, err := os.Stat(exe)
	if err != nil {
		return "unknown"
	}
	return fmt.Sprintf("%s:%d:%d", filepath.Clean(exe), info.Size(), info.ModTime().UTC().UnixNano())
}

func currentDaemonInfo() DaemonInfo {
	return DaemonInfo{
		Protocol: daemonProtocol,
		Version:  ActGUIVersion,
		BuildID:  daemonBuildID(),
		PID:      os.Getpid(),
	}
}

func validateDaemonPort(port string) (string, error) {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("%s must be a TCP port from 1 to 65535", actGUIPortFlag)
	}
	return strconv.Itoa(n), nil
}

func parseActGUIArgs(args []string) (string, []string, error) {
	port := defaultDaemonPort
	actArgs := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			actArgs = append(actArgs, args[i:]...)
			break
		}
		if arg == actGUIPortFlag {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("%s requires a port value", actGUIPortFlag)
			}
			port = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, actGUIPortFlag+"=") {
			port = strings.TrimPrefix(arg, actGUIPortFlag+"=")
			continue
		}
		actArgs = append(actArgs, arg)
	}

	port, err := validateDaemonPort(port)
	if err != nil {
		return "", nil, err
	}
	return port, actArgs, nil
}

func actGUIDataDir() (string, error) {
	home, _ := os.UserHomeDir()
	return actGUIDataDirFor(runtime.GOOS, os.Getenv, home)
}

func actGUIDataDirFor(goos string, getenv func(string) string, home string) (string, error) {
	switch goos {
	case "windows":
		if dir := getenv("APPDATA"); dir != "" {
			return filepath.Join(dir, "act-gui"), nil
		}
		if dir := getenv("LOCALAPPDATA"); dir != "" {
			return filepath.Join(dir, "act-gui"), nil
		}
	case "darwin":
		if home != "" {
			return filepath.Join(home, "Library", "Application Support", "act-gui"), nil
		}
	default:
		if dir := getenv("XDG_DATA_HOME"); dir != "" {
			return filepath.Join(dir, "act-gui"), nil
		}
		if home != "" {
			return filepath.Join(home, ".local", "share", "act-gui"), nil
		}
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "act-gui"), nil
}

func actGUIDatabasePath() (string, error) {
	dataDir, err := actGUIDataDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "act-gui.db"), nil
}

func broadcast(msg []byte) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	for client := range clients {
		if err := client.WriteMessage(websocket.TextMessage, msg); err != nil {
			_ = client.Close()
			delete(clients, client)
		}
	}
}

func probeDaemon(client *http.Client, baseURL string) (DaemonInfo, bool, error) {
	resp, err := client.Get(baseURL + "/ping")
	if err != nil {
		return DaemonInfo{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return DaemonInfo{}, true, fmt.Errorf("ping returned HTTP %d", resp.StatusCode)
	}

	var info DaemonInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return DaemonInfo{}, true, fmt.Errorf("unsupported daemon response; expected act-gui daemon protocol %d", daemonProtocol)
	}
	if info.Protocol != daemonProtocol {
		return info, true, fmt.Errorf("daemon protocol %d does not match required protocol %d", info.Protocol, daemonProtocol)
	}
	if info.Version != ActGUIVersion {
		return info, true, fmt.Errorf("daemon version %q does not match client version %q", info.Version, ActGUIVersion)
	}
	expectedBuildID := daemonBuildID()
	if info.BuildID != expectedBuildID {
		return info, true, fmt.Errorf("daemon build %q does not match client build %q", info.BuildID, expectedBuildID)
	}
	return info, true, nil
}

func ensureDaemon(baseURL string, port string) error {
	client := &http.Client{Timeout: 1 * time.Second}
	if _, reachable, err := probeDaemon(client, baseURL); err == nil {
		return nil
	} else if reachable {
		return fmt.Errorf("incompatible act-gui daemon at %s: %w; stop the old daemon or use %s to select another port", baseURL, err, actGUIPortFlag)
	}

	fmt.Println("Daemon not found, starting a new daemon...")
	if err := startDaemon(baseURL, port); err != nil {
		return err
	}
	return nil
}

func startDaemon(baseURL string, port string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, internalDaemonFlag, actGUIPortFlag, port)
	configureDaemonCommand(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	client := &http.Client{Timeout: 1 * time.Second}
	for i := 0; i < 15; i++ {
		if _, reachable, err := probeDaemon(client, baseURL); err == nil {
			return nil
		} else if reachable {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for daemon to start")
}

func firstFlagValue(args []string, longName, shortName string) string {
	longPrefix := "--" + longName + "="
	for i, arg := range args {
		if strings.HasPrefix(arg, longPrefix) {
			return strings.TrimPrefix(arg, longPrefix)
		}
		if arg == "--"+longName || arg == "-"+shortName {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if shortName != "" && strings.HasPrefix(arg, "-"+shortName) && len(arg) > len(shortName)+1 {
			return strings.TrimPrefix(arg, "-"+shortName)
		}
	}
	return ""
}

func inferActEvent(args []string) string {
	flagsWithValue := map[string]bool{
		"--actor": true, "--artifact-server-addr": true, "--artifact-server-path": true,
		"--artifact-server-port": true, "--cache-server-addr": true, "--cache-server-external-url": true,
		"--cache-server-path": true, "--cache-server-port": true, "--container-architecture": true,
		"--container-daemon-socket": true, "--container-options": true, "--defaultbranch": true,
		"--directory": true, "--env": true, "--env-file": true, "--eventpath": true,
		"--github-instance": true, "--input": true, "--input-file": true, "--job": true,
		"--local-repository": true, "--matrix": true, "--network": true, "--platform": true,
		"--remote-name": true, "--replace-ghe-action-token-with-github-com": true,
		"--secret": true, "--secret-file": true, "--var": true, "--var-file": true,
		"--workflows": true,
	}
	shortFlagsWithValue := map[rune]bool{
		'a': true, 'C': true, 'e': true, 'j': true, 'P': true, 's': true, 'W': true,
	}

	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--" {
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
		if strings.HasPrefix(arg, "--") {
			name, hasValue := strings.CutSuffix(arg, "=")
			if !hasValue {
				name, _, hasValue = strings.Cut(arg, "=")
			}
			if flagsWithValue[name] && !hasValue {
				skipNext = true
			}
			continue
		}
		if len(arg) >= 2 {
			flag := []rune(arg[1:])[0]
			if shortFlagsWithValue[flag] && len([]rune(arg)) == 2 {
				skipNext = true
			}
		}
	}
	return "push"
}

func buildStartRunPayload(args []string) StartRunPayload {
	event := inferActEvent(args)
	job := firstFlagValue(args, "job", "j")
	workflow := firstFlagValue(args, "workflows", "W")
	if workflow == "" {
		workflow = "local act workflow"
	}

	name := "act " + event
	if job != "" {
		name += " / " + job
	}

	branchBytes, _ := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	commitBytes, _ := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	return StartRunPayload{
		Name:      name,
		Workflow:  workflow,
		Event:     event,
		Branch:    strings.TrimSpace(string(branchBytes)),
		CommitSHA: strings.TrimSpace(string(commitBytes)),
		Jobs:      workflowJobsFromArgs(args),
	}
}

type LogPayload struct {
	RunID   uint   `json:"run_id"`
	Message string `json:"message"`
}

type StartRunPayload struct {
	Name      string            `json:"name"`
	Workflow  string            `json:"workflow"`
	Event     string            `json:"event"`
	Branch    string            `json:"branch"`
	CommitSHA string            `json:"commit_sha"`
	Jobs      []StartJobPayload `json:"jobs,omitempty"`
}

type StartRunResponse struct {
	RunID uint `json:"run_id"`
}

type FinishRunPayload struct {
	RunID  uint   `json:"run_id"`
	Status string `json:"status"`
}

func pipeToDaemon(r io.Reader, original io.Writer, client *http.Client, baseURL string, runID uint) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(original, line)
		payload := LogPayload{RunID: runID, Message: line}
		b, _ := json.Marshal(payload)
		resp, err := client.Post(baseURL+"/log", "application/json", bytes.NewBuffer(b))
		if err == nil {
			resp.Body.Close()
		}
	}
}

func postFinishRun(client *http.Client, baseURL string, runID uint, status string) error {
	if runID == 0 {
		return nil
	}
	finishPayload, _ := json.Marshal(FinishRunPayload{RunID: runID, Status: status})
	var lastErr error
	for i := 0; i < 3; i++ {
		resp, err := client.Post(baseURL+"/run/finish", "application/json", bytes.NewBuffer(finishPayload))
		if err == nil {
			resp.Body.Close()
			return nil
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	return lastErr
}

func watchRunCancellation(ctx context.Context, finish func(string)) func() {
	doneCh := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			finish("cancelled")
		case <-doneCh:
		}
	}()
	return func() {
		close(doneCh)
	}
}

func runCompletionStatus(ctx context.Context) string {
	if ctx.Err() != nil {
		return "cancelled"
	}
	return "success"
}

func runActChild(ctx context.Context, actArgs []string, stdout io.Writer, stderr io.Writer, client *http.Client, baseURL string, runID uint) (string, int) {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "act-gui: %v\n", err)
		return "failure", 1
	}

	cmdArgs := append([]string{internalRunnerFlag}, actArgs...)
	cmd := exec.CommandContext(ctx, exe, cmdArgs...)
	cmd.Stdin = os.Stdin

	childStdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(stderr, "act-gui: %v\n", err)
		return "failure", 1
	}
	childStderr, err := cmd.StderrPipe()
	if err != nil {
		fmt.Fprintf(stderr, "act-gui: %v\n", err)
		return "failure", 1
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "act-gui: %v\n", err)
		return "failure", 1
	}

	var pipeWG sync.WaitGroup
	pipeWG.Add(2)
	go func() {
		defer pipeWG.Done()
		pipeToDaemon(childStdout, stdout, client, baseURL, runID)
	}()
	go func() {
		defer pipeWG.Done()
		pipeToDaemon(childStderr, stderr, client, baseURL, runID)
	}()

	err = cmd.Wait()
	pipeWG.Wait()

	status := runCompletionStatus(ctx)
	exitCode := 0
	if err != nil {
		status = "failure"
		exitCode = 1
		if ctx.Err() != nil {
			status = "cancelled"
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			fmt.Fprintf(stderr, "act-gui: %v\n", err)
		}
	}
	return status, exitCode
}

func main() {
	port, actArgs, err := parseActGUIArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "act-gui: %v\n", err)
		os.Exit(2)
	}
	baseURL := daemonBaseURL(port)

	if len(actArgs) > 0 && actArgs[0] == internalRunnerFlag {
		ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stopSignals()
		os.Args = append([]string{os.Args[0]}, actArgs[1:]...)
		actcmd.Execute(ctx, ActGUIVersion)
		return
	}

	if len(actArgs) > 0 && actArgs[0] == internalDaemonFlag {
		dbPath, err := actGUIDatabasePath()
		if err != nil {
			panic(err)
		}
		db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
		if err != nil {
			panic(err)
		}
		db.AutoMigrate(&Run{}, &Job{}, &Step{}, &LogLine{})
		RegisterAPI(db)

		fmt.Println("Starting daemon on " + baseURL)
		fmt.Println("Using data directory " + filepath.Dir(dbPath))

		http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
			setAPIHeaders(w)
			json.NewEncoder(w).Encode(currentDaemonInfo())
		})

		http.HandleFunc("/run/start", func(w http.ResponseWriter, r *http.Request) {
			var payload StartRunPayload
			json.NewDecoder(r.Body).Decode(&payload)
			run := Run{
				Name:      payload.Name,
				Workflow:  payload.Workflow,
				Event:     payload.Event,
				Branch:    payload.Branch,
				CommitSHA: payload.CommitSHA,
			}
			db.Create(&run)
			for _, jobPayload := range payload.Jobs {
				jobID := jobPayload.JobID
				if jobID == "" {
					jobID = jobPayload.Name
				}
				if jobID == "" {
					continue
				}
				name := jobPayload.Name
				if name == "" {
					name = jobID
				}
				job := Job{
					RunID:  run.ID,
					JobID:  jobID,
					Name:   name,
					Status: "waiting",
					Needs:  encodeNeeds(jobPayload.Needs),
				}
				db.Create(&job)
			}
			json.NewEncoder(w).Encode(StartRunResponse{RunID: run.ID})
			broadcast([]byte(`{"event":"new_run"}`))
		})

		http.HandleFunc("/run/finish", func(w http.ResponseWriter, r *http.Request) {
			var payload FinishRunPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err == nil && payload.RunID > 0 {
				status := finishRun(db, payload.RunID, payload.Status)
				updateMsg, _ := json.Marshal(map[string]interface{}{
					"event":  "run_finished",
					"run_id": payload.RunID,
					"status": status,
				})
				broadcast(updateMsg)
			}
			w.WriteHeader(http.StatusOK)
		})

		http.HandleFunc("/log", func(w http.ResponseWriter, r *http.Request) {
			var payload LogPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
				ParseLogLine(db, payload.RunID, payload.Message)
				updateMsg, _ := json.Marshal(map[string]interface{}{
					"event":  "log",
					"run_id": payload.RunID,
				})
				broadcast(updateMsg)
			}
			w.WriteHeader(http.StatusOK)
		})

		http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
			ws, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			clientsMu.Lock()
			clients[ws] = true
			clientsMu.Unlock()
			defer func() {
				clientsMu.Lock()
				delete(clients, ws)
				clientsMu.Unlock()
				_ = ws.Close()
			}()
			for {
				if _, _, err := ws.NextReader(); err != nil {
					return
				}
			}
		})

		subFS, _ := fs.Sub(uiFiles, "ui/dist")
		fsHandler := http.FileServer(http.FS(subFS))

		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			f, err := subFS.Open(strings.TrimPrefix(r.URL.Path, "/"))
			if err != nil {
				// file not found, fallback to index.html for SPA
				index, _ := subFS.Open("index.html")
				if index != nil {
					stat, _ := index.Stat()
					http.ServeContent(w, r, "index.html", stat.ModTime(), index.(io.ReadSeeker))
					index.Close()
					return
				}
			} else {
				f.Close()
			}
			fsHandler.ServeHTTP(w, r)
		})

		if err := http.ListenAndServe(":"+port, nil); err != nil {
			fmt.Fprintf(os.Stderr, "act-gui daemon: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := ensureDaemon(baseURL, port); err != nil {
		fmt.Fprintf(os.Stderr, "act-gui: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("act-gui server: " + baseURL)

	client := &http.Client{Timeout: 2 * time.Second}

	runPayload, _ := json.Marshal(buildStartRunPayload(actArgs))
	resp, err := client.Post(baseURL+"/run/start", "application/json", bytes.NewBuffer(runPayload))
	var runID uint
	if err == nil {
		var startResp StartRunResponse
		json.NewDecoder(resp.Body).Decode(&startResp)
		resp.Body.Close()
		runID = startResp.RunID
	}

	finishOnce := sync.Once{}
	finish := func(status string) {
		finishOnce.Do(func() {
			if err := postFinishRun(client, baseURL, runID, status); err != nil {
				fmt.Fprintf(os.Stderr, "act-gui: failed to finish run: %v\n", err)
			}
		})
	}

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	status, exitCode := runActChild(ctx, actArgs, os.Stdout, os.Stderr, client, baseURL, runID)
	finish(status)
	time.Sleep(200 * time.Millisecond)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
