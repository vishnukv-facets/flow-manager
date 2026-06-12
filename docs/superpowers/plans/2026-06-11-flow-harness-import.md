# Flow Harness Import Plan

## Goal

Import the recent `Facets-cloud/flow` runtime features into Flow Manager without creating a second task/session system.

## Decisions

- Keep Flow Manager's existing `session_provider` field as the UI/API contract.
- Add `tasks.harness` as an internal runtime pin for compatibility with upstream harness/owner code.
- Derive empty harness values from `session_provider` so existing rows continue to work.
- Reuse the current auto-run implementation for `flow do --auto`; do not add a parallel headless runner.
- Add `$FLOW_TERM=bg` as a spawner backend that launches through the harness background interface.
- Import owner support incrementally: backend/store first, then server API/UI.

## Steps

1. Add DB support for `tasks.harness`, including migration coverage for the session-invariant table rebuild.
2. Add a harness package and resolver that maps current providers to harnesses.
3. Wire `flow do`, `flow do --auto`, transcript/session lookup, and launch commands through the resolver.
4. Add the background-agent backend for `FLOW_TERM=bg`.
5. Add owner data model and CLI commands backed by the harness launcher.
6. Add owner API and a compact UI surface that reuses existing task/tag views.
7. Run `make ui`, `go test ./...`, and targeted UI tests where changed.
