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
	actGUIPortFlag     = "--act-gui-port"
	defaultDaemonPort  = "18080"
	daemonHost         = "localhost"
)

var ActGUIVersion = "act-gui dev"

func daemonBaseURL(port string) string {
	return "http://" + daemonHost + ":" + port
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

func isDaemonRunning(baseURL string) bool {
	client := http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(baseURL + "/ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
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
	for i := 0; i < 15; i++ {
		if isDaemonRunning(baseURL) {
			return nil
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

func pipeToDaemon(r *os.File, original io.Writer, client *http.Client, baseURL string, runID uint) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(original, line)
		go func(l string) {
			payload := LogPayload{RunID: runID, Message: l}
			b, _ := json.Marshal(payload)
			resp, err := client.Post(baseURL+"/log", "application/json", bytes.NewBuffer(b))
			if err == nil {
				resp.Body.Close()
			}
		}(line)
	}
}

func postFinishRun(client *http.Client, baseURL string, runID uint, status string) {
	if runID == 0 {
		return
	}
	finishPayload, _ := json.Marshal(FinishRunPayload{RunID: runID, Status: status})
	resp, err := client.Post(baseURL+"/run/finish", "application/json", bytes.NewBuffer(finishPayload))
	if err == nil {
		resp.Body.Close()
	}
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

func main() {
	port, actArgs, err := parseActGUIArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "act-gui: %v\n", err)
		os.Exit(2)
	}
	baseURL := daemonBaseURL(port)

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
			w.Write([]byte("pong"))
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
				if jobPayload.Name == "" {
					continue
				}
				job := Job{
					RunID:  run.ID,
					Name:   jobPayload.Name,
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

		http.ListenAndServe(":"+port, nil)
		return
	}

	if !isDaemonRunning(baseURL) {
		fmt.Println("Daemon not found, starting a new daemon...")
		if err := startDaemon(baseURL, port); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start act-gui daemon: %v\n", err)
		}
	}
	if isDaemonRunning(baseURL) {
		fmt.Println("act-gui server: " + baseURL)
	}

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

	oldStdout := os.Stdout
	oldStderr := os.Stderr

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()

	os.Stdout = wOut
	os.Stderr = wErr

	go pipeToDaemon(rOut, oldStdout, client, baseURL, runID)
	go pipeToDaemon(rErr, oldStderr, client, baseURL, runID)

	finishOnce := sync.Once{}
	finish := func(status string) {
		finishOnce.Do(func() {
			postFinishRun(client, baseURL, runID, status)
		})
	}

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	stopCancellationWatch := watchRunCancellation(ctx, finish)

	os.Args = append([]string{os.Args[0]}, actArgs...)
	actcmd.Execute(ctx, ActGUIVersion)
	stopCancellationWatch()

	wOut.Close()
	wErr.Close()
	finish(runCompletionStatus(ctx))
	time.Sleep(200 * time.Millisecond)
}
