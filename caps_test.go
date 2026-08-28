package xui

import (
	"os"
	"testing"

	"github.com/pulseaiclub/xui/cell"
)

func TestDetectColorLevel(t *testing.T) {
	tests := []struct {
		name       string
		env        map[string]string // set these
		unset      []string          // must be absent for LookupEnv
		wantLevel  cell.ColorLevel
		wantForced bool
	}{
		{
			name:       "default basic",
			env:        map[string]string{"TERM": "xterm"},
			unset:      []string{"FORCE_COLOR", "NO_COLOR", "COLORTERM"},
			wantLevel:  cell.ColorBasic,
			wantForced: false,
		},
		{
			name:       "NO_COLOR empty",
			env:        map[string]string{"TERM": "xterm-256color", "NO_COLOR": ""},
			unset:      []string{"FORCE_COLOR", "COLORTERM"},
			wantLevel:  cell.ColorNone,
			wantForced: true,
		},
		{
			name:       "FORCE_COLOR overrides NO_COLOR",
			env:        map[string]string{"FORCE_COLOR": "2", "NO_COLOR": "1", "TERM": "dumb"},
			unset:      []string{"COLORTERM"},
			wantLevel:  cell.Color256,
			wantForced: true,
		},
		{
			name:       "FORCE_COLOR 0",
			env:        map[string]string{"FORCE_COLOR": "0", "COLORTERM": "truecolor", "TERM": "xterm"},
			unset:      []string{"NO_COLOR"},
			wantLevel:  cell.ColorNone,
			wantForced: true,
		},
		{
			name:       "FORCE_COLOR true allows upgrade",
			env:        map[string]string{"FORCE_COLOR": "true", "COLORTERM": "truecolor", "TERM": "xterm"},
			unset:      []string{"NO_COLOR"},
			wantLevel:  cell.ColorTrue,
			wantForced: false,
		},
		{
			name:       "FORCE_COLOR empty forces basic",
			env:        map[string]string{"FORCE_COLOR": "", "COLORTERM": "truecolor", "TERM": "xterm"},
			unset:      []string{"NO_COLOR"},
			wantLevel:  cell.ColorBasic,
			wantForced: true,
		},
		{
			name:       "FORCE_COLOR clamp high",
			env:        map[string]string{"FORCE_COLOR": "99", "TERM": "xterm"},
			unset:      []string{"NO_COLOR", "COLORTERM"},
			wantLevel:  cell.ColorTrue,
			wantForced: true,
		},
		{
			name:       "FORCE_COLOR clamp low",
			env:        map[string]string{"FORCE_COLOR": "-1", "TERM": "xterm"},
			unset:      []string{"NO_COLOR", "COLORTERM"},
			wantLevel:  cell.ColorNone,
			wantForced: true,
		},
		{
			name:       "COLORTERM truecolor",
			env:        map[string]string{"COLORTERM": "truecolor", "TERM": "xterm"},
			unset:      []string{"FORCE_COLOR", "NO_COLOR"},
			wantLevel:  cell.ColorTrue,
			wantForced: false,
		},
		{
			name:       "TERM 256color",
			env:        map[string]string{"TERM": "xterm-256color"},
			unset:      []string{"FORCE_COLOR", "NO_COLOR", "COLORTERM"},
			wantLevel:  cell.Color256,
			wantForced: false,
		},
		{
			name:       "TERM dumb",
			env:        map[string]string{"TERM": "dumb"},
			unset:      []string{"FORCE_COLOR", "NO_COLOR", "COLORTERM"},
			wantLevel:  cell.ColorNone,
			wantForced: true,
		},
		{
			name:       "TERM direct",
			env:        map[string]string{"TERM": "xterm-direct"},
			unset:      []string{"FORCE_COLOR", "NO_COLOR", "COLORTERM"},
			wantLevel:  cell.ColorTrue,
			wantForced: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range tt.unset {
				unsetenv(t, k)
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			level, forced := detectColorLevel()
			if level != tt.wantLevel || forced != tt.wantForced {
				t.Fatalf("detectColorLevel() = (%v, %v), want (%v, %v)",
					level, forced, tt.wantLevel, tt.wantForced)
			}
		})
	}
}

// unsetenv removes key for the duration of the test and restores afterward.
// t.Setenv cannot clear a variable that must be absent for LookupEnv.
func unsetenv(t *testing.T, key string) {
	t.Helper()
	orig, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, orig)
			return
		}
		_ = os.Unsetenv(key)
	})
}
