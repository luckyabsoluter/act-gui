package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

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
