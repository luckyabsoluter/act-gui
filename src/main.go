package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	actcmd "github.com/nektos/act/cmd"
)

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

	if actGUIHelpRequested(actArgs) {
		printActGUIHelp(os.Stdout)
		return
	}

	if actGUIVersionRequested(actArgs) {
		printActGUIVersion(os.Stdout)
		return
	}

	if actHelpRequested(actArgs) {
		ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stopSignals()
		os.Args = []string{os.Args[0], "--help"}
		actcmd.Execute(ctx, ActGUIVersion)
		return
	}

	if len(actArgs) > 0 && actArgs[0] == internalDaemonFlag {
		if err := runDaemon(port, baseURL); err != nil {
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
