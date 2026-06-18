package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

//go:embed ui/dist/*
//go:embed ui/dist/assets/*
var uiFiles embed.FS

var upgrader = websocket.Upgrader{}

var (
	clients   = make(map[*websocket.Conn]bool)
	clientsMu sync.Mutex
)

type DaemonInfo struct {
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
	PID      int    `json:"pid"`
}

func currentDaemonInfo() DaemonInfo {
	return DaemonInfo{
		Protocol: daemonProtocol,
		Version:  ActGUIVersion,
		PID:      os.Getpid(),
	}
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
	resp, err := client.Get(baseURL + "/version")
	if err != nil {
		return DaemonInfo{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return DaemonInfo{}, true, fmt.Errorf("version endpoint returned HTTP %d", resp.StatusCode)
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
	return info, true, nil
}

func ensureDaemon(baseURL string, host string, port string) error {
	client := &http.Client{Timeout: 1 * time.Second}
	if _, reachable, err := probeDaemon(client, baseURL); err == nil {
		return nil
	} else if reachable {
		return fmt.Errorf("incompatible act-gui daemon at %s: %w; stop the old daemon or use %s or %s to select another endpoint", baseURL, err, actGUIHostFlag, actGUIPortFlag)
	}

	fmt.Println("Daemon not found, starting a new daemon...")
	if err := startDaemon(baseURL, host, port); err != nil {
		return err
	}
	return nil
}

func startDaemon(baseURL string, host string, port string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, internalDaemonFlag, actGUIHostFlag, host, actGUIPortFlag, port)
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

func runDaemon(host string, port string, baseURL string) error {
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

	http.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		setAPIHeaders(w)
		json.NewEncoder(w).Encode(currentDaemonInfo())
	})
	http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
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

	return http.ListenAndServe(net.JoinHostPort(host, port), nil)
}
