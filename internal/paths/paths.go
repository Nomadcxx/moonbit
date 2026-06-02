package paths

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

func HomeDir() (string, error) {
	if home := os.Getenv("MOONBIT_HOME"); home != "" {
		return home, nil
	}

	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" && os.Geteuid() == 0 {
		userHome := filepath.Join("/home", sudoUser)
		if stat, err := os.Stat(userHome); err == nil && stat.IsDir() {
			return userHome, nil
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
