package xui

import (
	"os"
	"strconv"
	"strings"

	"github.com/pulseaiclub/xui/cell"
)

// detectColorLevel derives a color baseline from the environment.
// The bool is true when the user forced a hard ceiling that in-band
// probes must not exceed (numeric FORCE_COLOR, NO_COLOR, TERM=dumb).
//
// Precedence: FORCE_COLOR > NO_COLOR > TERM=dumb > WT_SESSION > COLORTERM > TERM.
func detectColorLevel() (cell.ColorLevel, bool) {
	if fc, ok := os.LookupEnv("FORCE_COLOR"); ok {
		switch strings.ToLower(strings.TrimSpace(fc)) {
		case "0", "false":
			return cell.ColorNone, true
		case "1", "":
			// Empty historically means "force basic"; numeric 1 is a hard cap.
			return cell.ColorBasic, true
		case "2":
			return cell.Color256, true
		case "3":
			return cell.ColorTrue, true
		case "true":
			// Enable color but let env hints (and later probes) raise the level.
			return maxLevel(envColorHint(), cell.ColorBasic), false
		default:
			if n, err := strconv.Atoi(fc); err == nil {
				return cell.ColorLevel(clampInt(n, 0, 3)), true
			}
			// Unrecognized value: enable color, do not hard-cap.
			return maxLevel(envColorHint(), cell.ColorBasic), false
		}
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		// Spec: presence alone disables color, even when empty.
		return cell.ColorNone, true
	}
	if os.Getenv("TERM") == "dumb" {
		return cell.ColorNone, true
	}
	return envColorHint(), false
}

// envColorHint reads WT_SESSION / COLORTERM / TERM for a non-forced baseline.
func envColorHint() cell.ColorLevel {
	// Windows Terminal hands neither TERM nor COLORTERM to a PowerShell
	// shell, but sets WT_SESSION for every process it spawns. It is checked
	// before TERM: a 256color TERM there is a shell default, not a limit of
	// the panel. FORCE_COLOR, NO_COLOR and TERM=dumb still win — those return
	// before this hint is consulted.
	if strings.TrimSpace(os.Getenv("WT_SESSION")) != "" {
		return cell.ColorTrue
	}
	switch os.Getenv("COLORTERM") {
	case "truecolor", "24bit":
		return cell.ColorTrue
	}
	term := os.Getenv("TERM")
	if strings.Contains(term, "truecolor") || strings.Contains(term, "direct") {
		return cell.ColorTrue
	}
	if strings.Contains(term, "256color") {
		return cell.Color256
	}
	return cell.ColorBasic
}

func maxLevel(a, b cell.ColorLevel) cell.ColorLevel {
	if a > b {
		return a
	}
	return b
}

func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
