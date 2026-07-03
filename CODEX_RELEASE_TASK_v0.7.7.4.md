# ZXY Panel V0.7.7.4 Release Assets Sync Task

Release tag: `v0.7.7.4`

Version: `0.7.7.4-ui-version-sync-polish-agent-xray`

Package: `zxy-panel-v0.7.7.4-ui-version-sync-polish.zip`

SHA256: `0f0855223cc3a297a030abcdca8b88a0b33cb69502dd3b25f1300d5c1df49d63`

## Scope

This task only syncs release assets and the version manifest.

Allowed changes:

- `version.json`
- `releases/v0.7.7.4/`
- `CODEX_RELEASE_TASK_v0.7.7.4.md`
- `docs/releases/v0.7.7.4.md`

## Included Release Notes

- Fix frontend dashboard display still showing `V0.7.7.2`.
- Fix version card display still showing `0.7.7.2-client-sync-fix-agent-xray`.
- Sync frontend, backend, Agent, health, and system information version to `0.7.7.4-ui-version-sync-polish-agent-xray`.
- Rebuild `frontend/dist` so production does not continue loading old JS.
- Preserve V0.7.7.3 Clash subscription binding fix.
- Preserve V0.7.7.2 BindingCheck, Agent apply validation, and Doctor deep checks.

## Explicit Non-Changes

- Does not modify `backend/`.
- Does not modify `agent/`.
- Does not modify `frontend/src/`.
- Does not modify install or deploy scripts.
- Does not modify Xray config generation.
- Does not modify Clash subscription logic.
- Does not modify customer management logic.
- Does not clear user data.

If validation fails, report the failure reason only and do not repair source code without explicit authorization.
