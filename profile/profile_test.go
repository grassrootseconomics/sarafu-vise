package profile

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/stretchr/testify/require"
)

func TestInsertOrShift(t *testing.T) {
	tests := []struct {
		name     string
		profile  Profile
		index    int
		value    string
		expected []string
	}{
		{
			name:     "Insert within range",
			profile:  Profile{ProfileItems: []string{"A", "B", "C"}, Max: 5},
			index:    1,
			value:    "X",
			expected: []string{"A", "X"},
		},
		{
			name:     "Insert beyond range",
			profile:  Profile{ProfileItems: []string{"A"}, Max: 5},
			index:    3,
			value:    "Y",
			expected: []string{"A", "0", "0", "Y"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.profile
			p.InsertOrShift(tt.index, tt.value)
			require.NotNil(t, p.ProfileItems)
			assert.Equal(t, tt.expected, p.ProfileItems)
		})
	}
}
