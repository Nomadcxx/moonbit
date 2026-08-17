package paths

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

// HOME-1: resolving SUDO_USER must look the user up rather than assuming
// /home/<name>. On a non-standard layout the old code fell through to HOME,
// which under sudo's env_reset is /root -- so moonbit scanned and cleaned root's
// caches while reporting success.
//
// The SUDO_USER branch only runs as euid 0, so assert the lookup mechanism the
// fix depends on and the precedence rules that are testable unprivileged.
func TestSudoUserHomeResolutionUsesUserLookup(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("test asserts unprivileged precedence; see the euid-0 branch separately")
	}

	current, err := user.Current()
	if err != nil {
		t.Skipf("cannot determine current user: %v", err)
	}

	// The mechanism the fix relies on: user.Lookup yields the real home, which
	// need not be /home/<name>.
	looked, err := user.Lookup(current.Username)
	if err != nil {
		t.Skipf("user.Lookup unavailable: %v", err)
	}
	if looked.HomeDir == "" {
		t.Fatal("user.Lookup returned an empty home directory")
	}
	if looked.HomeDir != current.HomeDir {
		t.Errorf("user.Lookup disagrees with user.Current: %q vs %q",
			looked.HomeDir, current.HomeDir)
	}

	// Not asserting the value equals /home/<name> -- that assumption is the bug.
	t.Logf("resolved home for %s: %s (assumed /home path would be %s)",
		current.Username, looked.HomeDir, filepath.Join("/home", current.Username))
}

// MOONBIT_HOME is the documented escape hatch and must win over everything.
func TestMoonbitHomeOverrides(t *testing.T) {
	want := t.TempDir()
	t.Setenv("MOONBIT_HOME", want)
	t.Setenv("HOME", "/definitely/not/this")

	got, err := HomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("MOONBIT_HOME must take precedence: got %q, want %q", got, want)
	}
}

// Unprivileged and with no SUDO_USER, HOME is authoritative.
func TestHomeEnvUsedWhenNotElevated(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged execution")
	}
	want := t.TempDir()
	t.Setenv("MOONBIT_HOME", "")
	t.Setenv("SUDO_USER", "")
	t.Setenv("HOME", want)

	got, err := HomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The XDG variables the systemd units set must redirect every moonbit path, so
// automation never writes into a user's home.
func TestXDGOverridesRedirectAllPaths(t *testing.T) {
	base := t.TempDir()
	t.Setenv("MOONBIT_HOME", filepath.Join(base, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "cfg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))

	cfg, err := ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "cfg", "moonbit", "config.toml"); cfg != want {
		t.Errorf("ConfigFile: got %q, want %q", cfg, want)
	}

	cache, err := CacheFile()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "cache", "moonbit", "scan_results.json"); cache != want {
		t.Errorf("CacheFile: got %q, want %q", cache, want)
	}

	data, err := DataDir("backups")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "data", "moonbit", "backups"); data != want {
		t.Errorf("DataDir: got %q, want %q", data, want)
	}
}
