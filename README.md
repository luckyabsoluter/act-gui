# act-gui

act-gui is a local web UI for running GitHub Actions workflows through [`act`](https://github.com/nektos/act).

It keeps act-compatible command-line arguments, starts or connects to a local daemon, and shows workflow runs in a GitHub Actions-like interface with workflows, runs, jobs, steps, logs, status, and run history management.

## Status

This project is under active development. See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for architecture and maintenance details.

## Usage

Run act-gui with act-compatible arguments:

```bash
./act-gui -W src/testdata/workflows/test.yml
```

Use `--act-gui-port` to run or connect to the local daemon on a different port:

```bash
./act-gui --act-gui-port 27979 -W src/testdata/workflows/test.yml
```

On startup, act-gui prints the web server address:

```text
act-gui server: http://localhost:18080
```

Open that address to inspect workflows, runs, jobs, steps, and logs.

## Daemon Model

act-gui uses a local daemon for the web server and runtime state. A normal CLI invocation will start the daemon if needed, attach to it, register a new run, stream logs, and detach when the act execution finishes.

The daemon listens on:

```text
http://localhost:18080
```

The default port is `18080`. Use `--act-gui-port <port>` to select another daemon port for both the CLI invocation and the daemon it starts.

The internal daemon flag is reserved for act-gui itself:

```text
--act-gui-daemon
```

## Runtime Data

Runtime data is stored in the user's application data directory, not in the current repository.

Current SQLite database locations:

- Windows: `%APPDATA%\act-gui\act-gui.db`
- macOS: `~/Library/Application Support/act-gui/act-gui.db`
- Linux: `$XDG_DATA_HOME/act-gui/act-gui.db`, or `~/.local/share/act-gui/act-gui.db`

This prevents normal act-gui runs from dirtying the project tree.

## Run Management

The run browser shows workflows and runs by default. Destructive controls are hidden until management mode is enabled.

Use `Manage` in the UI to reveal:

- individual run deletion
- clear history
- artifact deletion, where available

Deletion actions use in-app confirmation dialogs instead of browser-native alert, prompt, or confirm dialogs.

## Development

Common checks:

```bash
go test ./...
cd src/ui
npm test
npm run build
cd ../..
go build -o act-gui ./src
```

Application source lives under `src`. Frontend source lives under `src/ui`. The production frontend bundle is built to `src/ui/dist` and embedded into the Go executable.

When changing frontend behavior, rebuild the UI before rebuilding `act-gui`.

## License Notes

act-gui is licensed under the MIT License. See [LICENSE](LICENSE).

Third-party license notes are tracked in [docs/THIRD_PARTY_NOTICES.md](docs/THIRD_PARTY_NOTICES.md).
