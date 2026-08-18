package paths

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

// homeFromPasswd resolves a home directory through the passwd database rather
// than assuming /home/<name>. Non-standard layouts (LDAP/SSSD, /export/home,
// per-team roots) break that assumption, and the failure is silent: the caller
// falls through to HOME, which is /root under an elevation.
//
// lookup is user.Lookup (by name) or user.LookupId (by numeric uid).
func homeFromPasswd(lookup func(string) (*user.User, error), key string) string {
	u, err := lookup(key)
	if err != nil || u.HomeDir == "" {
		return ""
	}
	if stat, err := os.Stat(u.HomeDir); err != nil || !stat.IsDir() {
		return ""
	}
	return u.HomeDir
}

// existingDir returns path when it is a directory, otherwise "".
func existingDir(path string) string {
	if stat, err := os.Stat(path); err == nil && stat.IsDir() {
		return path
	}
	return ""
}

func HomeDir() (string, error) {
	if home := os.Getenv("MOONBIT_HOME"); home != "" {
		return home, nil
	}

	// Recover the human behind an elevation. Both sudo and pkexec drop the
	// original HOME, which under euid 0 leaves /root -- moonbit would then scan
	// and clean root's caches while reporting success, and the user's actual
	// caches would never be touched.
	if os.Geteuid() == 0 {
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
			if home := homeFromPasswd(user.Lookup, sudoUser); home != "" {
				return home, nil
			}
			// Conventional layout, as a last resort.
			if home := existingDir(filepath.Join("/home", sudoUser)); home != "" {
				return home, nil
			}
			return "", fmt.Errorf("cannot resolve home directory for SUDO_USER=%q; "+
				"set MOONBIT_HOME to the intended home directory", sudoUser)
		}
		// pkexec, which the desktop launcher uses, sets PKEXEC_UID and never
		// SUDO_USER. Without this branch a launcher-started moonbit cleans
		// root's caches instead of yours.
		if pkexecUID := os.Getenv("PKEXEC_UID"); pkexecUID != "" {
			if home := homeFromPasswd(user.LookupId, pkexecUID); home != "" {
				return home, nil
			}
			return "", fmt.Errorf("cannot resolve home directory for PKEXEC_UID=%q; "+
				"set MOONBIT_HOME to the intended home directory", pkexecUID)
		}
	}

	if home := os.Getenv("HOME"); home != "" {
		return home, nil
	}

	if current, err := user.Current(); err == nil && current.HomeDir != "" {
		return current.HomeDir, nil
	}

	if os.Geteuid() == 0 {
		return "/root", nil
	}

	return "", fmt.Errorf("unable to determine home directory; set HOME or MOONBIT_HOME")
}

func ConfigFile() (string, error) {
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return filepath.Join(configHome, "moonbit", "config.toml"), nil
	}
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "moonbit", "config.toml"), nil
}

func CacheFile() (string, error) {
	if cacheHome := os.Getenv("XDG_CACHE_HOME"); cacheHome != "" {
		return filepath.Join(cacheHome, "moonbit", "scan_results.json"), nil
	}
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "moonbit", "scan_results.json"), nil
}

func DataDir(parts ...string) (string, error) {
	var base string
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		base = filepath.Join(dataHome, "moonbit")
	} else {
		home, err := HomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share", "moonbit")
	}

	allParts := append([]string{base}, parts...)
	return filepath.Join(allParts...), nil
}
