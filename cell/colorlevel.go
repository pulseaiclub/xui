package cell

// ColorLevel is the terminal's color capability, aligned with chalk /
// supports-color levels.
type ColorLevel uint8

const (
	// ColorNone emits no color SGR sequences (monochrome).
	ColorNone ColorLevel = 0
	// ColorBasic is the classic 8/16 ANSI palette.
	ColorBasic ColorLevel = 1
	// Color256 is the 256-color palette (6×6×6 cube + gray ramp).
	Color256 ColorLevel = 2
	// ColorTrue is 24-bit truecolor.
	ColorTrue ColorLevel = 3
)
