# act-gui Development Notes

This document records the current architecture choices and the maintenance risks that follow from them.

## Status

This project is under active development. The current implementation favors a practical local workflow over a stable public API.

Important maintenance notes:

- act is used as a Go library, not as an external executable.
- act output is parsed non-invasively to infer jobs, steps, logs, and status. This keeps act-gui decoupled from act internals, but it can break if act output changes.
- Parts of the UI are copied from Gitea to provide a mature Actions-like interface. This improves the UI quickly, but copied source requires careful license and maintenance handling.

## Goals

act-gui is a local GUI for `act`. It should keep act-compatible command-line arguments, run workflows through the act backend, and expose a GitHub Actions-like web interface for workflow runs, jobs, steps, logs, and progress.

The daemon owns the local web server and runtime state. Individual `act-gui` CLI invocations should be able to attach to the daemon, start a run, stream output, detach cleanly, and preserve run history without depending on the current project directory for runtime data.

The daemon listens on `localhost:27979` by default. `--act-gui-host <host>` and `--act-gui-port <port>` are act-gui-only arguments: the CLI strips them before handing control to act, starts or connects to the daemon on that endpoint, and passes the same host and port to the internal daemon process.

## Why Go

Go is used for the top-level application because it supports a simple single-file distribution model. The current executable embeds the frontend bundle and can run without a separate Node, Python, or web-server installation.

This also matches act's implementation language. act is a Go project, so act-gui can call act as a Go library instead of shelling out to a separate `act` binary. That keeps the installation path simpler and allows closer lifecycle integration, including context cancellation and output capture.

## act Integration

act is currently used as a library through `github.com/nektos/act/cmd`. This is easier to ship and maintain than managing an external binary dependency. It also lets act-gui pass a Go context into act execution, which is required for reliable cancellation handling.

The fragile part is output parsing. act-gui currently treats act output as a non-invasive integration surface and parses log lines to infer jobs, steps, status transitions, and logs. This avoids patching act or maintaining a fork, but it is inherently brittle:

- act output wording or formatting can change.
- status inference depends on emitted text.
- job and step boundaries are reconstructed from log lines.
- regression tests are required for every parser or lifecycle bug that reaches the UI.

When changing parser behavior, prefer a small test that captures the exact act output shape being handled. Do not rely only on manual UI checks.

## Gitea UI Source

The UI intentionally borrows from Gitea because Gitea is also a Go application with a mature Actions UI. That made it a practical reference for a GitHub Actions-like local interface without inventing every interaction from scratch.

However, copied source is expensive to maintain. The `src/ui/src/gitea`, `src/ui/src/gitea_css`, and related copied assets should be treated as imported third-party code, not as ordinary local code. Be careful when editing them:

- Keep local changes small and documented by commit history.
- Prefer wrappers or adapters in `src/ui/src/App.vue` and local modules when possible.
- Avoid broad formatting changes in copied files.
- Preserve upstream license and copyright notices.
- Expect upstream drift. Future Gitea updates may not apply cleanly.

If a future change needs substantial behavior from Gitea, first consider whether the needed piece can be copied narrowly or reimplemented against act-gui's data model. Copying more source increases long-term maintenance cost.

## Frontend Bundle

The frontend is built under `src/ui` and embedded into the Go executable from `src/ui/dist`. After frontend changes, run the UI build before rebuilding the executable:

```bash
cd src/ui
npm run build
cd ../..
version="$(go run ./tools/buildversion)"
go build -ldflags "-X main.ActGUIVersion=${version}" -o act-gui ./src
```

For logic that can be isolated from Vue components, put it in a plain TypeScript module and cover it with `npm test`. This keeps UI regressions testable without adding a browser test framework for every small state calculation.

## Runtime Data

Runtime state belongs in the user's application data directory, not in the current repository. The daemon stores its SQLite database under the OS-specific app data path so running act-gui from a project does not dirty the project tree.

## Maintenance Rules

- Keep act CLI compatibility first. act-gui arguments should continue to map naturally to act.
- Do not introduce invasive act changes unless the parser surface becomes unmaintainable.
- Treat copied Gitea code as third-party source with extra review care.
- Add regression tests for cancellation, log parsing, status derivation, run deletion, and workflow tab state.
- Rebuild `act-gui` after changes that affect embedded frontend or Go runtime behavior.
