# act-gui UI

This directory contains the Vue and TypeScript frontend for act-gui.

The UI is built with Vite and embedded into the Go executable from `src/ui/dist`.

## Commands

```bash
npm test
npm run build
```

After rebuilding the frontend, rebuild the root executable:

```bash
cd ../..
go build -o act-gui ./src
```

## Notes

- `src/App.vue` owns the act-gui shell, workflow/run browser, management mode, and run detail routing.
- `src/workflow-tabs.ts` contains testable workflow tab state calculation.
- `src/gitea` and `src/gitea_css` contain copied or adapted Gitea UI code. Keep changes there narrow and preserve upstream license notices.

See `../../docs/DEVELOPMENT.md` and `../../docs/THIRD_PARTY_NOTICES.md` before making broad UI source changes.
