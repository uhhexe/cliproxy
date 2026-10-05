# PLAN: 01 — Quota dashboard + reset-soonest routing

## Handoff
Cody watched Theo show a forked CLIProxyAPI with a "Quota Management" page (provider roll-up
cards like "7-day Fable 5 · 409% of 500%", a "Ledger" view of every credential with 5-hour /
7-day / Fable meters, masked emails with a Show-emails toggle) and asked for all of it, plus
**routing that sends work to the account whose quota resets soonest first**. He runs the
upstream proxy today through the Easy CLI Proxy desktop app (`/Applications/EasyCLIProxyAPI.app`,
core v7.3.17 at `~/Library/Application Support/com.cpa.gui/cpa-core/`, auth dir
`~/.cli-proxy-api`, port 8317). He chose "its own page only" — no ümux panel.
Three forks exist under his account: `uhhexe/cliproxy` (Go proxy, this repo),
`uhhexe/cliproxy-dashboard` (React management page → `management.html`),
`uhhexe/easycliproxy` (Tauri desktop app; touch only if Stage 3 research says the app
cannot run our core). Upstream already ships ~70% of the page: route `/quota`, provider tabs,
per-credential meters incl. "7-day Fable 5", plan labels, header counts, refresh, collapsible
sidebar. **Done:** forks + gap map. **Next:** Stage 1 task 1.1.

## Approach (chosen) — extend the forks in place, new code in new files
Keep every change additive (new files, small hooks into existing ones) so the forks can keep
merging upstream, which releases daily. Quota truth for routing comes from the proxy itself:
the passive `anthropic-ratelimit-unified-*` / `x-codex-*` headers it already records, plus a
new background prober that hits the same usage endpoints the dashboard uses, so the selector
knows reset times before an account has served a request.
**Rejected:** a ümux panel reading the proxy (Cody picked "own page only") · a sidecar
service that polls quota and rewrites the proxy's weights (a second router running beside the
real one; drifts) · a plugin scheduler (`sdk/pluginapi` candidates carry no quota state).
Decision logged to Engram: `build-plan/cliproxy/quota-dashboard`.

## Phase Goal
On Cody's Mac, `http://127.0.0.1:8317/management.html#/quota` shows Theo's page (roll-up cards,
Ledger rows, masked emails, toggle), and routing set to "Reset soonest first" sends Claude and
Codex requests to the account with remaining quota that resets soonest.

## Ground truth (from the 2026-10-05 gap map — re-verify line numbers, upstream moves daily)
Dashboard (`uhhexe/cliproxy-dashboard/src`):
- Page: `features/quota/QuotaPage.tsx`; header `features/quota/components/QuotaHeader.tsx`;
  cards `QuotaCard.tsx`; meters `QuotaMeter.tsx` (green ≥70, amber ≥30, red <30); reset label
  `QuotaResetLabel.tsx`; state `features/quota/uiState.ts` (sessionStorage); constants
  `features/quota/constants.ts` (`QUOTA_TAB_ORDER`, page size 20); grouping `logic.ts`.
- Names: `utils/quota/identity.ts` `getQuotaDisplayName`. Times: `utils/quota/relativeTime.ts`
  (`buildResetDisplay`, `formatInstantShort`). Schedules: `features/quota/resetSchedule.ts`
  (`collectQuotaRowInstants`, `nextRecoveryMs`).
- Headline windows: Claude `seven-day-fable` (secondary `seven-day`) in
  `providers/claude/data.ts`; Codex `weekly` in `providers/codex/data.ts`; xAI billing row with
  `periodType: 'weekly'`; Kimi row `kimi_quota.weekly_limit` (`utils/quota/builders.ts`).
- Loading is click-per-card today; batch loader `hooks/useQuotaBatchLoader.ts`; auto-load
  pattern `providers/devin/useDevinQuotaAutoLoad.ts`.
- Sidebar collapse: `components/layout/MainLayout.tsx` (plain `useState`, not persisted).
- Repo rules: `AGENTS.md`; every UI string gets keys in all 4 locale files.
- Checks: `bun install --frozen-lockfile && bun run verify` (test + lint + build); `bun run type-check`.
Proxy (`uhhexe/cliproxy`):
- Selectors `sdk/cliproxy/auth/selector.go` (RoundRobin, WeightedRoundRobin, FillFirst;
  `getSelectorAvailableAuths` drops cooled-down creds; `preferCodexWebsocketAuths`).
- Strategy wiring `sdk/cliproxy/service_config.go` (normalize + `newRoutingSelector`),
  `internal/config/config_types.go`, `internal/api/handlers/management/config_basic.go`,
  `config.example.yaml`. A non-built-in selector takes the slower `selector.Pick` path
  automatically (`conductor_selection.go`, `isBuiltInSelector`) — no scheduler change needed.
- Passive quota: `sdk/cliproxy/auth/quota_signals.go` → `Auth.Quota.Signals/ObservedAt` and
  `ModelStates[model].Quota.Signals` (`types.go`), written in `MarkResult`
  (`conductor_cooldown.go`); exposed by `GET /v8/management/credentials` (`auth_files.go`).
- Token-injecting passthrough used by the dashboard: `internal/api/handlers/management/api_tools.go`.
- Panel serving: `internal/managementasset/updater.go` — `management.panel-github-repository`
  points auto-update at a fork's releases; `management.disable-auto-update-panel: true` stops
  overwrites.
- Checks: `gofmt -l .` (empty), `go test ./sdk/cliproxy/auth/... ./internal/...`,
  `go build -o /tmp/cpa-test ./cmd/server`.

## Stage 1 — the page looks like Theo's (repo: uhhexe/cliproxy-dashboard)
- [ ] 1.1 Masked credential names + "Show emails" toggle
  - Where: `utils/quota/identity.ts` (new `maskCredentialName`), `features/quota/uiState.ts`
    (`showEmails`, default false), `QuotaHeader.tsx` (toggle button left of Refresh),
    `QuotaCard.tsx`, `QuotaTimeline.tsx`, 4 locale files, new `tests/quotaMaskName.test.ts`.
  - Mask rule: keep provider prefix + first char of local part and first char of domain,
    keep the TLD and `.json`: `claude-tom@lab.dev.json` → `claude-t•••@l•••.dev.json`.
  - Verify: `bun run verify` exits 0; the new test asserts that example plus a name with no `@`
    (unchanged) and a toggle-on case (raw name).
  - Fence: touches only the files listed; no change to how names are sent to the API.
  - Tier: build
- [ ] 1.2 Auto-load quota for every credential (not just the visible page)
  - Where: new `features/quota/useQuotaAutoLoadAll.ts` modeled on `useDevinQuotaAutoLoad.ts`,
    calling `useQuotaBatchLoader.loadQuota`; mount in `QuotaPage.tsx`; Refresh reloads all.
    Concurrency cap 4, skip entries loaded < 60 s ago.
  - Verify: `bun run verify`; new `tests/quotaAutoLoadAll.test.ts` drives the scheduler with a
    fake loader: 10 entries → 10 loads, never > 4 in flight, second call within 60 s → 0 loads.
  - Fence: no change to provider fetchers or the api-call client.
  - Tier: build
- [ ] 1.3 Provider roll-up cards
  - Where: new pure `features/quota/rollup.ts` (`buildProviderRollups(entries, quotaByType)` →
    per provider: headline label, sum of remaining %, N×100 cap, per-credential segments with
    level, soonest reset instant, optional secondary line), new
    `components/QuotaRollupCard.tsx` + `.module.scss`, rendered in a horizontal strip above
    the list in `QuotaPage.tsx`; secondary line uses `components/ui/Collapsible` ("Show").
  - Headline per provider: Claude "7-day Fable 5" (secondary "7-day limit"); Codex "Weekly
    limit"; xAI "Weekly limit"; Kimi "Weekly limit"; Antigravity: lowest-remaining model group.
    Credentials with no data count toward N but render a grey segment; all-unknown shows `--`.
  - Verify: `bun run verify`; new `tests/quotaRollup.test.ts`: 5 Claude creds at
    58/100/100/51/100 → "409% of 500%", 5 segments, levels amber/green/green/amber/green, soonest
    reset = earliest instant; Codex 17/0/0 of 3 → "17% of 300%"; xAI unknown → `--` of 100%.
  - Fence: reuse `QuotaMeter` thresholds + `formatInstantShort`; no new colour tokens.
  - Tier: build
- [ ] 1.4 Ledger view + view selector
  - Where: `constants.ts` (`QUOTA_VIEW_MODES = ['ledger','cards']`), `uiState.ts` (`viewMode`,
    default `ledger`), new `components/QuotaLedger.tsx` + `.module.scss`: one section per
    provider ("Claude 5" header), one row per credential — masked name + plan label on the
    left, that provider's windows as columns (label, % remaining right-aligned, bar, "in 1 day ·
    09/12, 23:00" under it). Selector is a `Select` next to the sort select in `QuotaPage.tsx`.
  - Verify: `bun run verify`; new `tests/quotaLedgerRendering.test.ts` (`renderToStaticMarkup`)
    shows provider headers with counts, 3 Claude columns in order Fable / 5-hour / 7-day, and
    the cards grid when `viewMode='cards'`.
  - Fence: provider Body components untouched except exporting their row builders if needed.
  - Tier: build
- [ ] 1.5 Reset text polish + sidebar memory
  - Where: `QuotaResetLabel.tsx` → "No reset pending" when `buildResetDisplay` is null;
    `formatInstantShort` → `MM/DD, HH:mm`; `MainLayout.tsx` collapse state persisted in
    localStorage (try/catch); locale keys.
  - Verify: `bun run verify`; existing `quotaRelativeTime` tests updated for the comma; a test
    for the null → "No reset pending" path.
  - Fence: no other layout change.
  - Tier: build
- [ ] 1.6 Release workflow publishes `management.html` from the fork
  - Where: `.github/workflows/release.yml` already builds and renames; confirm it runs on the
    fork; tag `v1.25.3-uhh.1`.
  - Verify: `gh release view v1.25.3-uhh.1 -R uhhexe/cliproxy-dashboard --json assets -q '.assets[].name'`
    lists `management.html`.
  - Fence: no change to upstream release logic beyond what a fork needs.
  - Tier: build

## Stage 2 — reset-soonest routing (repo: uhhexe/cliproxy)
- [ ] 2.1 Quota view over recorded signals
  - Where: new `sdk/cliproxy/auth/quota_view.go` + `_test.go`: `QuotaWindows(auth, model)
    []QuotaWindow{Name, UsedFraction, ResetAt, Exhausted, Known}`. Claude from
    `Anthropic-Ratelimit-Unified-{5h,7d,7d_oi}-{Utilization,Reset,Status}`; Codex from
    `X-Codex-{Primary,Secondary}-{Used-Percent,Reset-At,Reset-After-Seconds,Window-Minutes}`,
    `X-Codex-Limit-Reached`. Prefer `ModelStates[model].Quota.Signals`, fall back to
    `auth.Quota.Signals`; a reset already in the past or `ObservedAt` older than 6 h = unknown.
  - Verify: `go test ./sdk/cliproxy/auth -run QuotaView -v` passes with table cases for both
    providers, stale data, and a passed reset.
  - Fence: read-only over existing structs; no change to `quota_signals.go` recording.
  - Tier: build
- [ ] 2.2 Background quota prober (so routing knows before first use)
  - Where: new `sdk/cliproxy/quotaprobe/` service started from the service builder when
    `routing.strategy == reset-soonest` (or `quota-probe.enabled: true`): every 10 min ± 60 s
    jitter per Claude/Codex credential, call the same usage endpoints the dashboard uses
    (Claude `GET https://api.anthropic.com/api/oauth/usage`, header
    `anthropic-beta: oauth-2025-04-20`; Codex `GET https://chatgpt.com/backend-api/wham/usage`),
    reusing the token/refresh logic in `api_tools.go`; translate the JSON into the same signal
    keys 2.1 reads and store them through the existing quota-signal path. Config keys in
    `config_types.go` + `config.example.yaml`.
  - Verify: `go test ./sdk/cliproxy/quotaprobe/...` with an `httptest` server returning a
    recorded Claude and Codex payload → `QuotaWindows` shows the expected reset/utilization;
    a 401/403 marks the cred unknown and backs off to 60 min.
  - Fence: never more than one in-flight probe per credential; never probes when the strategy
    is not reset-soonest and the flag is off; no new external dependency.
  - Tier: build
- [ ] 2.3 `ResetSoonestSelector` + registration
  - Where: `selector.go` (new type; start from `getSelectorAvailableAuths`, drop
    `Exhausted` creds, sort known ones by the soonest `ResetAt` among windows with quota left —
    use the binding window: the one with the highest `UsedFraction`; ties by ID; unknown creds
    after known ones in embedded round-robin; keep `preferCodexWebsocketAuths`),
    `service_config.go` (both switches), `config_types.go` comment, `config_basic.go`,
    `config.example.yaml`. Dashboard repo: strategy option "Reset soonest first" in
    `types/visualConfig.ts`, `SectionNetwork.tsx`, `useVisualConfig.ts`, `DashboardPage.tsx`,
    `features/config/searchIndex.ts`, 4 locales.
  - Verify: `go test ./sdk/cliproxy/auth -run ResetSoonest -v`: 3 creds resetting in 1 d / 4 d /
    3 h with quota left → picks the 3 h one; the 3 h one exhausted → picks 1 d; all unknown →
    rotates like round-robin; all blocked → the existing cooldown error. Plus `go build` and
    `bun run verify` in the dashboard repo.
  - Fence: no change to the built-in scheduler fast path.
  - Tier: build

## Stage 3 — running on Cody's Mac
- [ ] 3.1 Research: can Easy CLI Proxy run our core?
  - Where: `uhhexe/easycliproxy` `src-tauri/src/app_update.rs` (custom download mirrors,
    `VersionDownloadSource::Custom`), its config writer (does it regenerate `config.yaml` from
    `config.toml` on start, and does it reject an unknown `routing-strategy`?).
  - Verify: a written answer in this PLAN under "Stage 3 findings": which mirror URL format
    serves `CLIProxyAPI_<ver>_darwin_aarch64.tar.gz` from `uhhexe/cliproxy` releases, and how
    `reset-soonest` + `panel-github-repository` survive an app restart.
  - Fence: read only. If the app cannot do it, the fallback is a small `easycliproxy` change
    (strategy list + default mirror) — planned as 3.1b, built only after this answer.
  - Tier: research
- [ ] 3.2 Release the proxy fork
  - Where: `.github/workflows/release.yaml` on the fork; tag `v8.0.15-uhh.1`.
  - Verify: `gh release view v8.0.15-uhh.1 -R uhhexe/cliproxy` lists the darwin_aarch64 tarball.
  - Tier: build
- [ ] 3.3 Install + cold press (Claude, never Cody)
  - Back up `~/Library/Application Support/com.cpa.gui/` (config.toml, cpa-core/config.yaml,
    current binary) to a dated folder first. Point the app at the fork per 3.1, set
    `management.panel-github-repository: uhhexe/cliproxy-dashboard` and routing
    `reset-soonest`, restart the core.
  - Verify: `curl -s 127.0.0.1:8317/management.html | grep -c maskCredentialName`-equivalent
    marker from the fork build ≥ 1; core `-h` prints the `-uhh` version; in the built-in browser
    the Quota page shows roll-up cards and Ledger rows for the 3 Codex creds; core log shows
    picks following the soonest-reset Codex credential across 5 test requests.
  - Rollback: restore the backup folder and restart the app.
  - Fence: never touch `~/.cli-proxy-api/*.json` credential files.
  - Tier: build (install) · review (cold press)

## Risks & tripwires
- Upstream ships daily; fork drift — tripwire: a weekly `git merge upstream/main` touching any
  file we changed conflicts in more than 3 hunks — fallback: move our logic further into new
  files and keep the hooks one-liners.
- Usage endpoints change or refuse probes — tripwire: 2.2 tests pass but live probes return
  4xx at 3.3 — fallback: the selector runs on passive headers only (known after first use).
- Pooling several Claude Max logins through one proxy can get accounts flagged by Anthropic
  — tripwire: a credential's probe starts returning 401/403 or `permission_error` — fallback:
  stop probing that credential, tell Cody in the day chat.
- Easy CLI Proxy overwrites our core on its own update — tripwire: core `-h` version loses
  `-uhh` after an app update — fallback: 3.1b (point the app's default mirror at the fork).

## Stage 3 findings
(filled in by 3.1)
