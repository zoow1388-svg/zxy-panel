# Codex Release Task: ZXY Panel V0.7.7.5 Stability Polish

Repository: `zoow1388-svg/zxy-panel`

Target version: `0.7.7.5-stability-polish-agent-xray`

Release tag: `v0.7.7.5`

Release assets:

1. `zxy-panel-v0.7.7.5-stability-polish.zip`
2. `version-v0.7.7.5.json`
3. `SHA256SUMS-v0.7.7.5`
4. `CODEX_RELEASE_TASK_v0.7.7.5.md`

## Strict source protection

This is a release publish task. Do not freely refactor code.

Allowed release updates:

- `version.json`
- `README.md`
- `CHANGELOG.md`
- `scripts/build-fast-release.sh`
- `docs/releases/v0.7.7.5.md`
- `releases/v0.7.7.5/*`
- release assets listed above

Forbidden unless explicitly authorized:

- `backend/internal/xray/config.go`
- `backend/internal/api/subscription.go`
- `backend/internal/api/agent.go`
- `backend/internal/api/clients.go`
- `agent/cmd/agent/main.go`
- `deploy/install.sh`
- `install.sh`
- `scripts/zxy-panel`
- dependency upgrades
- full-repository formatting

## Publish steps

1. Create branch `release/v0.7.7.5-stability-polish`.
2. Place release assets under `releases/v0.7.7.5/`.
3. Replace root `version.json` with `version-v0.7.7.5.json` content.
4. Create PR titled `Release v0.7.7.5 stability polish`.
5. Before merge, confirm `git diff --name-only` contains only expected release files.
6. After merge, confirm raw `main/version.json` points to V0.7.7.5.
7. Create or update GitHub Release `v0.7.7.5` and upload the release assets.

If validation fails, report the issue. Do not modify core source to fix it without approval.
