# Category file preview (issue #18)

**Goal:** On the TUI category-select screen, Shift+X opens a popup that lists the files the highlighted category would clean.

**Constraint:** the select screen must look exactly as it does today. Nothing is resized or reflowed; the popup is drawn over the finished screen.

**Not:** a new view mode, a split pane, or a list of every checked category at once.

## Approaches

1. **Modal popup overlay (chosen).** Shift+X draws a bordered box centred over the screen. The logo, panel border and footer stay visible around it. Closing it restores the screen byte-for-byte.
2. Split pane on the right. Rejected: it shrinks the category viewport and changes the layout of the select screen.
3. Full-screen file page. Rejected: hides the category list the user is choosing from.

## Behaviour

- `Shift+X` (`X` / `shift+x`) on a category row opens the popup. On Select All / Clean / Back it does nothing.
- The popup is modal: ↑/↓ scroll one file, PgUp/PgDn a page, Home/End jump. Keys never reach the category list.
- `Esc`, `q` or `Shift+X` close it. `Esc` closes the popup only; a second `Esc` leaves the screen.
- Header: category name, file count, total size. Each row: path (left-truncated with `…`) and size. Bottom: `start–end of N`.
- At most 15 rows and 90 columns, so it floats over the panel instead of covering the logo.
- Only the visible page is rendered, so large categories need no cap.

## Implementation

- `Model.filePreviewOpen`, `Model.filePreviewOffset`. Stays in `ModeSelect`; `renderSelect()` is unchanged.
- `View()` renders the screen as before, then `overlayCenter()` splices the popup over it with `x/ansi` `Truncate`/`TruncateLeft`. Those calls keep ANSI escapes, so colours either side of the popup are preserved.
- `previewFiles()` filters `scanResults.ScanResults.Files` by `CategoryName`.
- Tests in `internal/ui/ui_test.go`: modal key handling, open only on a category row, filtering, scroll clamping, and an overlay check that the screen height and the rows above and below the popup are unchanged.

## Verification

```
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/ui -run 'TestFilePreview'
go build -o moonbit cmd/main.go
# TUI: scan → select categories → Shift+X → ↑/↓ scroll → Esc closes
```
