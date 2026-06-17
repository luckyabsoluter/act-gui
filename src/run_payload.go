package main

import (
	"os/exec"
	"strings"
)

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
