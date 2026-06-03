# Third-Party Notices

This project uses and includes third-party software. This document summarizes the high-risk dependencies and copied source that maintainers need to track.

act-gui's root project license is declared in the root `LICENSE` file. This document tracks third-party license notes and copied source that require separate maintenance attention.

## act

- Project: `github.com/nektos/act`
- Usage: Go library dependency used as the workflow execution backend.
- License: MIT License.
- Local license source checked: Go module cache for the resolved `github.com/nektos/act` module license.

act's MIT license permits use, copying, modification, distribution, sublicensing, and sale, provided the copyright notice and permission notice are included in copies or substantial portions of the software.

## Gitea

- Project: Gitea
- Copied source locations: `src/ui/src/gitea`, `src/ui/src/gitea_css`, and related Gitea-derived UI assets.
- Source reference: Gitea commit `79810ba2e37a5b5b7840a7737a877fc7f1ea7c38`.
- Usage: UI source and styling used as the basis for the Actions-like workflow interface.
- License: MIT License.
- Local license source checked: `../gitea_src/LICENSE`.

Gitea's license notice begins with:

```text
Copyright (c) 2016 The Gitea Authors
Copyright (c) 2015 The Gogs Authors
```

Gitea's MIT license permits use, copying, modification, distribution, sublicensing, and sale, provided the copyright notice and permission notice are included in copies or substantial portions of the software.

## Copied Source Maintenance

Copied Gitea source is not the same as a normal package dependency. It will not receive upstream fixes automatically and can drift from upstream quickly. Maintainers should:

- Preserve license and copyright notices.
- Keep a clear record of local modifications.
- Avoid copying additional upstream source unless necessary.
- Prefer small adapters around copied source instead of broad rewrites inside it.
- Re-check upstream license notices when refreshing copied files.

## Generated Dependency Notices

The Go and npm dependency graphs include many transitive dependencies. Before publishing binary or source distributions, generate a complete dependency notice from the exact release build inputs:

```bash
go list -m -json all
cd src/ui
npm ls --all
```

The generated notice should be reviewed alongside this file because this file intentionally highlights the main architectural dependencies rather than every transitive package.
