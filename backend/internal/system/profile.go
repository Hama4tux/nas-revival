package system

// Profile describes board facts that sysfs cannot tell us, such as which
// fan headers of a multi-channel controller are actually wired.
type Profile struct {
	// Fans maps hwmon fan IDs to display labels. When non-empty, only these
	// fans are reported, so unwired channels do not appear as stopped fans.
	Fans map[string]string
}

// profiles is keyed by the device-tree model string.
var profiles = map[string]Profile{
	// Owner-confirmed 2026-11-25: one 20 mm fan on LM85 fan1.
	"ZyXEL NSA310": {Fans: map[string]string{"lm85/fan1": "Ventilátor"}},
}

func profileFor(board *string) Profile {
	if board == nil {
		return Profile{}
	}
	return profiles[*board]
}
