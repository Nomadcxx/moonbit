package cli

import (
	"io"
	"testing"
	"time"

	"github.com/Nomadcxx/moonbit/internal/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDurationRejectsNonPositive(t *testing.T) {
	for _, input := range []string{"0s", "-1s", "0d", "-1d"} {
		t.Run(input, func(t *testing.T) {
			_, err := parseDuration(input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be positive")
		})
	}
}

func TestPerformCleanUsesLiveClean(t *testing.T) {
	originalClean := daemonCleanSession
	originalState := daemonState
	originalOut := daemonOut
	defer func() {
		daemonCleanSession = originalClean
		daemonState = originalState
		daemonOut = originalOut
	}()

	daemonState = &DaemonState{StartTime: time.Now(), logger: (*audit.Logger)(nil)}
	daemonOut = io.Discard

	var gotDryRun bool
	daemonCleanSession = func(dryRun bool) error {
		gotDryRun = dryRun
		return nil
	}

	performClean()

	assert.False(t, gotDryRun, "scheduled daemon clean should actually clean")
	assert.Equal(t, 1, daemonState.stats().CleanCount)
}
