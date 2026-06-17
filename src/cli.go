package main

import (
	"fmt"
	"io"
	"runtime/debug"
	"strconv"
	"strings"
)

const (
	internalDaemonFlag = "--act-gui-daemon"
	internalRunnerFlag = "--act-gui-runner"
	actGUIPortFlag     = "--act-gui-port"
	actHelpFlag        = "--act-help"
	actGUIVersionFlag  = "--version"
	actModulePath      = "github.com/nektos/act"
	defaultDaemonPort  = "27979"
	daemonHost         = "localhost"
	daemonProtocol     = 1
)

var ActGUIVersion = "act-gui dev"

func daemonBaseURL(port string) string {
	return "http://" + daemonHost + ":" + port
}

func actGUIHelpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func actHelpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == actHelpFlag {
			return true
		}
	}
	return false
}

func actGUIVersionRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == actGUIVersionFlag {
			return true
		}
	}
	return false
}

func moduleVersion(module *debug.Module) string {
	if module.Replace != nil && module.Replace.Version != "" {
		return module.Replace.Version
	}
	if module.Version != "" {
		return module.Version
	}
	return "unknown"
}

func actLibraryVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == actModulePath {
			return moduleVersion(dep)
		}
	}
	return "unknown"
}

func printActGUIVersion(w io.Writer) {
	fmt.Fprintf(w, "act-gui: %s\n", ActGUIVersion)
	fmt.Fprintf(w, "act library: %s %s\n", actModulePath, actLibraryVersion())
}

func printActGUIHelp(w io.Writer) {
	fmt.Fprint(w, `act-gui runs GitHub Actions workflows through act and shows a local web UI.

Usage:
  act-gui [act-gui options] [act options] [event]

Act arguments:
  Any act options and event arguments can be passed after act-gui options.
  Use --act-help to inspect the underlying act options.

Act-gui options:
  --act-gui-port <port>  Run or connect to the local act-gui daemon on this port.
  --act-help             Show act help.
  --version              Show act-gui and act library versions.
  -h, --help             Show act-gui help.

Examples:
  act-gui -W src/testdata/workflows/test.yml
  act-gui --act-gui-port 27979 -W src/testdata/workflows/test.yml
  act-gui --act-help
  act-gui --version
`)
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
