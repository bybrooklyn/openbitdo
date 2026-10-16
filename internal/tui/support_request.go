package tui

import (
	"fmt"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// supportRequestBody assembles a GitHub-issue-ready markdown body from the
// current diagnostic report. This is the direct extension of the issue #15
// fix: that fix explained *why* checks fail for an unconfirmed device; this
// turns the same result into a well-formed report instead of leaving the
// user to screenshot the diagnostics screen (which is literally what
// triggered issue #15 in the first place).
func supportRequestBody(device core.AppDevice, result protocol.DiagProbeResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "**Device:** %s (%s, vid=%#04x pid=%#04x)\n", device.DisplayName, device.Name, device.VidPid.VID, device.VidPid.PID)
	fmt.Fprintf(&b, "**Support tier:** %s\n", device.SupportTier)
	fmt.Fprintf(&b, "**Protocol family:** %s\n", device.ProtocolFamily)
	fmt.Fprintf(&b, "**Evidence:** %s\n\n", device.Evidence)

	failing := make([]protocol.DiagCommandStatus, 0, len(result.CommandChecks))
	for _, c := range result.CommandChecks {
		if !c.OK {
			failing = append(failing, c)
		}
	}
	answered := len(result.CommandChecks) - len(failing)
	fmt.Fprintf(&b, "**Result:** %d of %d diagnostic checks answered.\n\n", answered, len(result.CommandChecks))
	if len(failing) == 0 {
		b.WriteString("All diagnostic checks passed.\n")
		return b.String()
	}

	if answered > 0 {
		b.WriteString("**Answered:**\n\n")
		for _, c := range result.CommandChecks {
			if c.OK {
				fmt.Fprintf(&b, "- `%s` — %s\n", c.Command, c.Detail)
			}
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "**Failing checks (%d/%d):**\n\n", len(failing), len(result.CommandChecks))
	for _, c := range failing {
		fmt.Fprintf(&b, "- `%s` — %s (confidence=%s, experimental=%v)\n", c.Command, c.Detail, c.Confidence, c.IsExperimental)
	}

	return b.String()
}
