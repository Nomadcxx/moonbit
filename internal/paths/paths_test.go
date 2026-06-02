package paths

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHomeDirUsesMoonbitHomeWhenSet(t *testing.T) {
	t.Setenv("MOONBIT_HOME", "/tmp/moonbit-home")
	t.Setenv("HOME", "")

	home, err := HomeDir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/moonbit-home", home)
}

func TestConfigFileUsesXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")

	path, err := ConfigFile()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/xdg-config", "moonbit", "config.toml"), path)
}

func TestCacheFileUsesXDGCacheHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")

	path, err := CacheFile()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/xdg-cache", "moonbit", "scan_results.json"), path)
}

func TestDataDirUsesXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")

	path, err := DataDir("logs")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/xdg-data", "moonbit", "logs"), path)
}

func TestHomeDirWorksWithoutHomeEnv(t *testing.T) {
	t.Setenv("MOONBIT_HOME", "")
	t.Setenv("HOME", "")

	home, err := HomeDir()
	require.NoError(t, err)
	assert.NotEmpty(t, home)
	assert.True(t, filepath.IsAbs(home))
	_, _ = os.Stat(home)
}
