# Category file preview (issue #18)

**Goal:** On the TUI category-select screen, Shift+X toggles a right-hand pane that lists the files the highlighted category would clean.

**Not:** a new view mode, always-on split, or a list of every checked category at once.

## Approaches

1. **Toggle split pane (chosen).** Shift+X opens/closes a pane on the right. ↑/↓ still move the category cursor; the pane follows the highlighted row. PgUp/PgDn scroll the file list. Space/Enter stay as they are.
2. Full-screen file page. Matches the issue’s “add a page” wording, but hides the category list the user is choosing from. Rejected — Nomad asked for a box on the right.
3. Always-on split when the terminal is wide. No key, cluttered on a typical 80-col session. Rejected.

## Behaviour

- Binding: `Shift+X` (`X` or `shift+x`). Lowercase `x` is unused. Select screen only.
- Pane shows files for the **highlighted** category (cursor), not the union of `[X]` checked categories.
- Each line: path (truncated to pane width) and size.
- Header: category name, file count, total size.
- Cursor on Select All / Clean / Back: hint to highlight a category.
- Cap at 200 files, then `+ N more`. Deep scans can be huge; this is a preview.
- Esc / leaving the select screen closes the pane.
- Footer: `Shift+X Preview`; when open, also `PgUp/PgDn Scroll files`.

## Implementation

- `Model.filePreviewOpen bool` + `filePreviewViewport viewport.Model`. Stay in `ModeSelect`.
- `previewFiles()` filters `scanResults.ScanResults.Files` by `CategoryName`.
- `renderSelect()` `lipgloss.JoinHorizontal` when the pane is open; shrink `categoryViewport` width.
- Tests in `internal/ui/ui_test.go` for toggle, filter, empty hint, cap, and rendered path.
- Closes #18.

## Verification

```
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/ui -run 'TestFilePreview'
go build -o moonbit cmd/main.go
# TUI: scan → select categories → Shift+X → ↑/↓ → pane follows; Shift+X closes
```
