# App Cache Deep-Clean Design

**Goal:** Improve deep-clean space savings with conservative app-cache targets while avoiding invasive categories such as model caches, browser caches, Steam shader/download caches, Teams, and user session history.

**Architecture:** Add a small app-cache rule layer on top of the existing category scanner. Rules declare app roots plus safe leaf directories, so MoonBit scans only cache/log/crash artifacts and never broad app data directories. Scan cache entries should retain category/risk provenance so quick/deep filtering does not rely on path-prefix guesses.

**Scope:**
- Add IDE/editor cache targets for Cursor, VS Code, Code OSS, VSCodium, Antigravity, and Sublime Text.
- Add AI/agent desktop/tool cache targets for Claude Desktop/CLI, opencode, Goose, ChatGPT Nativefier, Copilot Nativefier, and Cursor agent.
- Add Wine/compat-layer cleanup targets for Bottles and Lutris temp/log/crash artifacts.
- Add non-browser Electron app cache targets where the safe leaves are well-known, excluding Teams.
- Add language/build cache expansion for bun, pnpm, yarn, deno, uv, node-gyp, clangd, ccache, golangci-lint, TypeScript, and Playwright with conservative risk defaults.
- Add Flatpak app-cache scanning for generic cache/tmp/fontconfig paths while excluding Steam and shader/download cache paths.
- Preserve opencode session history and state: never clean `storage`, `project`, `repos`, `plugins`, config files, databases, or session/history-like directories by default.
- Preserve Wine prefix state: never clean Bottles/Lutris prefixes wholesale, runners, downloaded installers, game saves, `AppData`, registries, or compatibility-layer assets by default.
- Do not add model cache categories. Exclude HuggingFace, Torch, Chroma, ONNX/model stores, and similar re-download-heavy caches from default deep-clean scope.
- Do not add browser cache categories beyond existing behavior. Avoid expanding Chromium/Firefox profile cache cleanup in this pass.

**Safety Rules:**
- Clean only allowlisted cache leaves such as `Cache`, `Code Cache`, `GPUCache`, `DawnGraphiteCache`, `DawnWebGPUCache`, `CachedData`, `CachedExtensionVSIXs`, `Crashpad`, `logs`, `tmp`, and tool-output directories with age limits.
- Treat logs, crash dumps, snapshots, and tool output as age-limited unless the directory is already a pure cache.
- Never clean `IndexedDB`, `Local Storage`, `Session Storage`, `User`, `Workspaces`, extensions, plugins, databases, attachments, profiles, auth/session data, opencode storage/project/repos, or app config.
- For Bottles/Lutris/Wine, clean only temp/log/crash leaves such as `drive_c/windows/temp`, `drive_c/users/*/Temp`, age-limited logs, and crash dumps. Do not clean `steamapps`, `compatdata`, runners, prefixes, bottle definitions, game directories, saves, registries, shader caches, or download caches.
- Keep higher-cost caches off by default or deep-only. Playwright browser binaries and old tool versions should be medium risk, not quick-clean defaults.

**Testing:** Add fake-home scanner tests for Cursor, Claude, opencode, Signal-like restricted apps, Flatpak Steam exclusions, and model/browser cache exclusions. Verify quick/deep mode uses stored category provenance rather than path-prefix heuristics.
