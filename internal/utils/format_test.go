package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHumanizeBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    uint64
		expected string
	}{
		{"Zero bytes", 0, "0 B"},
		{"Bytes", 512, "512 B"},
		{"Kilobytes", 1024, "1.0 KB"},
		{"Kilobytes decimal", 1536, "1.5 KB"},
		{"Megabytes", 1048576, "1.0 MB"},
		{"Megabytes decimal", 1572864, "1.5 MB"},
		{"Gigabytes", 1073741824, "1.0 GB"},
		{"Gigabytes decimal", 1610612736, "1.5 GB"},
		{"Large value", 1099511627776, "1024.0 GB"},
		{"Just under KB", 1023, "1023 B"},
		{"Just under MB", 1048575, "1024.0 KB"},
		{"Just under GB", 1073741823, "1024.0 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HumanizeBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}
