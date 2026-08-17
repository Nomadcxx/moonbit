package validation

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
)

// DefaultMaxCacheAge bounds how long a scan result stays usable. Past this the
// recorded paths have had long enough to be replaced by something else.
const DefaultMaxCacheAge = 24 * time.Hour

// DropReason explains why a cached file did not survive revalidation.
type DropReason string

const (
	DropUnknownCategory DropReason = "category not in config"
	DropOutsideCategory DropReason = "path outside configured category paths"
	DropFilterMismatch  DropReason = "path no longer matches category filters"
	DropExcluded        DropReason = "path matches category exclude pattern"
	DropTooRecent       DropReason = "file newer than category min_age_days"
	DropMissing         DropReason = "file no longer exists"
	DropNotRegular      DropReason = "not a regular file"
	DropChanged         DropReason = "size or mtime changed since scan"
)

// Report summarises what the gate removed, so callers can tell the user why the
// delete list shrank instead of silently cleaning less than was scanned.
type Report struct {
	Accepted int
	Dropped  map[DropReason]int
	// Examples holds up to a few dropped paths per reason for diagnostics.
	Examples map[DropReason][]string
}

func newReport() *Report {
	return &Report{
		Dropped:  make(map[DropReason]int),
		Examples: make(map[DropReason][]string),
	}
}

func (r *Report) drop(reason DropReason, path string) {
	r.Dropped[reason]++
	if len(r.Examples[reason]) < 3 {
		r.Examples[reason] = append(r.Examples[reason], path)
	}
}

// TotalDropped returns how many cached entries were rejected.
func (r *Report) TotalDropped() int {
	total := 0
	for _, n := range r.Dropped {
		total += n
	}
	return total
}

// Summary renders a short human-readable description of what was dropped.
func (r *Report) Summary() string {
	if r.TotalDropped() == 0 {
		return ""
	}
	parts := make([]string, 0, len(r.Dropped))
	for reason, n := range r.Dropped {
		parts = append(parts, fmt.Sprintf("%d %s", n, reason))
	}
	sortStrings(parts)
	return strings.Join(parts, "; ")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// CacheOptions tunes revalidation. The zero value uses DefaultMaxCacheAge and
// the current wall clock.
type CacheOptions struct {
	MaxAge time.Duration
	Now    time.Time
}

func (o CacheOptions) maxAge() time.Duration {
	if o.MaxAge <= 0 {
		return DefaultMaxCacheAge
	}
	return o.MaxAge
}

func (o CacheOptions) now() time.Time {
	if o.Now.IsZero() {
		return time.Now()
	}
	return o.Now
}

// resolvedCategory is the authoritative, config-derived view of one category,
// with globs expanded and filters compiled once.
type resolvedCategory struct {
	cat      *config.Category
	roots    []string // literal, cleaned
	resolved []string // symlink-resolved counterpart of roots (may be shorter)
	filters  []*regexp.Regexp
	excludes []*regexp.Regexp
}

func resolveCategories(categories []config.Category) map[string]*resolvedCategory {
	out := make(map[string]*resolvedCategory, len(categories))
	for i := range categories {
		cat := &categories[i]
		rc := &resolvedCategory{cat: cat}

		for _, p := range cat.Paths {
			var expanded []string
			if strings.ContainsAny(p, "*?[") {
				matches, err := filepath.Glob(p)
				if err != nil {
					continue
				}
				expanded = matches
			} else {
				expanded = []string{p}
			}
			for _, e := range expanded {
				clean := filepath.Clean(e)
				rc.roots = append(rc.roots, clean)
				if real, err := filepath.EvalSymlinks(clean); err == nil {
					rc.resolved = append(rc.resolved, real)
				} else {
					// Unresolvable root cannot authorise anything; keep the slices
					// aligned so a root is only usable when both halves exist.
					rc.resolved = append(rc.resolved, "")
				}
			}
		}

		for _, f := range cat.Filters {
			if re, err := regexp.Compile(f); err == nil {
				rc.filters = append(rc.filters, re)
			}
		}
		for _, e := range cat.ExcludePatterns {
			if re, err := regexp.Compile(e); err == nil {
				rc.excludes = append(rc.excludes, re)
			}
		}

		out[normalizeName(cat.Name)] = rc
	}
	return out
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// contains reports whether child is root itself or lies beneath it.
func contains(root, child string) bool {
	if root == "" {
		return false
	}
	if root == child {
		return true
	}
	if !strings.HasSuffix(root, string(filepath.Separator)) {
		root += string(filepath.Separator)
	}
	return strings.HasPrefix(child, root)
}

// authorises reports whether the category's configured paths cover this file.
//
// Both the literal and the symlink-resolved path must be covered. The literal
// check is what config actually authorised. The resolved check closes the escape
// where a symlinked *parent* directory inside an authorised tree points somewhere
// else entirely -- Lstat on the full path reports a perfectly ordinary regular
// file in that case, so the file-level check cannot catch it.
func (rc *resolvedCategory) authorises(literal, resolved string) bool {
	for i, root := range rc.roots {
		if !contains(root, literal) {
			continue
		}
		realRoot := rc.resolved[i]
		if realRoot == "" {
			continue
		}
		if contains(realRoot, resolved) {
			return true
		}
	}
	return false
}

func (rc *resolvedCategory) matchesFilters(path string) bool {
	if len(rc.filters) == 0 {
		return true
	}
	base := filepath.Base(path)
	slashed := filepath.ToSlash(path)
	for _, re := range rc.filters {
		if re.MatchString(base) || re.MatchString(slashed) {
			return true
		}
	}
	return false
}

func (rc *resolvedCategory) isExcluded(path string) bool {
	slashed := filepath.ToSlash(path)
	for _, re := range rc.excludes {
		if re.MatchString(slashed) {
			return true
		}
	}
	return false
}

// RevalidateCache re-derives the authoritative delete list from config.
//
// The session cache lives in the invoking user's home directory and is therefore
// user-writable, but `moonbit clean --force` consumes it as root. Treating it as
// trusted makes it an unauthenticated instruction list for root file deletion,
// and lets it self-attest the very fields the safety checks read (Risk, Size,
// ShredEnabled). This gate takes paths from the cache as *claims* and everything
// else from config.
//
// It is deliberately subtractive: it only ever removes entries from the delete
// list, never adds or rewrites paths. The worst outcome of a bug here is
// cleaning less than expected.
func RevalidateCache(cache *config.SessionCache, categories []config.Category, opts CacheOptions) (*config.SessionCache, *Report, error) {
	if cache == nil || cache.ScanResults == nil {
		return nil, nil, fmt.Errorf("no scan results to validate")
	}

	if cache.ScannedAt.IsZero() {
		return nil, nil, fmt.Errorf("scan cache has no timestamp; re-run 'moonbit scan'")
	}
	age := opts.now().Sub(cache.ScannedAt)
	if age > opts.maxAge() {
		return nil, nil, fmt.Errorf("scan results are %s old (limit %s); re-run 'moonbit scan'",
			age.Round(time.Minute), opts.maxAge())
	}
	if age < -5*time.Minute {
		return nil, nil, fmt.Errorf("scan cache is timestamped in the future; re-run 'moonbit scan'")
	}

	resolved := resolveCategories(categories)
	report := newReport()

	files := cache.ScanResults.Files
	accepted := make([]config.FileInfo, 0, len(files))
	var totalSize uint64
	aggregateRisk := config.Low

	for _, file := range files {
		// Provenance is required. A cache without it cannot be checked against
		// config at all, so refuse the whole thing rather than quietly deleting
		// nothing or, worse, trusting it.
		if file.CategoryName == "" {
			return nil, nil, fmt.Errorf(
				"scan cache predates category provenance and cannot be verified; re-run 'moonbit scan'")
		}

		rc, ok := resolved[normalizeName(file.CategoryName)]
		if !ok {
			report.drop(DropUnknownCategory, file.Path)
			continue
		}

		literal := filepath.Clean(file.Path)

		// Stat before path checks: cheapest way to discard the common case of a
		// file that is simply gone, and it is what makes EvalSymlinks meaningful.
		info, err := os.Lstat(literal)
		if err != nil {
			report.drop(DropMissing, file.Path)
			continue
		}
		if !info.Mode().IsRegular() {
			report.drop(DropNotRegular, file.Path)
			continue
		}

		realPath, err := filepath.EvalSymlinks(literal)
		if err != nil {
			report.drop(DropMissing, file.Path)
			continue
		}

		if !rc.authorises(literal, realPath) {
			report.drop(DropOutsideCategory, file.Path)
			continue
		}
		if rc.isExcluded(literal) {
			report.drop(DropExcluded, file.Path)
			continue
		}
		if !rc.matchesFilters(literal) {
			report.drop(DropFilterMismatch, file.Path)
			continue
		}

		if rc.cat.MinAgeDays > 0 {
			minAge := time.Duration(rc.cat.MinAgeDays) * 24 * time.Hour
			if opts.now().Sub(info.ModTime()) < minAge {
				report.drop(DropTooRecent, file.Path)
				continue
			}
		}

		// The file must be the one that was scanned, not merely a file at the
		// same path. Anything that moved since the scan is out of scope.
		if uint64(info.Size()) != file.Size {
			report.drop(DropChanged, file.Path)
			continue
		}
		if file.ModTime != "" && info.ModTime().Format(time.RFC3339) != file.ModTime {
			report.drop(DropChanged, file.Path)
			continue
		}

		// Authoritative metadata comes from config, never from the cache.
		verified := file
		verified.Path = literal
		verified.Size = uint64(info.Size())
		verified.CategoryName = rc.cat.Name
		verified.CategoryRisk = rc.cat.Risk
		verified.CategorySelected = rc.cat.Selected
		verified.CategoryShred = rc.cat.ShredEnabled
		verified.CategoryAction = rc.cat.Action

		if rc.cat.Risk > aggregateRisk {
			aggregateRisk = rc.cat.Risk
		}
		totalSize += verified.Size
		accepted = append(accepted, verified)
		report.Accepted++
	}

	out := &config.SessionCache{
		ScanResults: &config.Category{
			Name:      cache.ScanResults.Name,
			Files:     accepted,
			FileCount: len(accepted),
			Size:      totalSize,
			Risk:      aggregateRisk,
			// Never honoured at the aggregate level: the cache must not be able to
			// turn on shredding. Per-file CategoryShred carries the config value.
			ShredEnabled: false,
		},
		TotalSize:  totalSize,
		TotalFiles: len(accepted),
		ScannedAt:  cache.ScannedAt,
	}
	return out, report, nil
}
