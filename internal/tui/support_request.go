package tui

import (
	"fmt"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// supportRequestBody assembles a GitHub-issue-ready markdown body from the
// current diagnostic report, for a candidate-readonly/detect-only device.
// This is the direct extension of the issue #15 fix: that fix explained
// *why* every check fails for an unconfirmed device; this turns that same
// explanation into a well-formed bug report instead of leaving the user to
// screenshot the raw diagnostics screen (which is literally what triggered
// issue #15 in the first place).
func supportRequestBody(device core.AppDevice, result protocol.DiagProbeResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "**Device:** %s (vid=%#04x pid=%#04x)\n", device.Name, device.VidPid.VID, device.VidPid.PID)
	fmt.Fprintf(&b, "**Support tier:** %s\n", device.SupportTier)
	fmt.Fprintf(&b, "**Protocol family:** %s\n", device.ProtocolFamily)
	fmt.Fprintf(&b, "**Evidence:** %s\n\n", device.Evidence)

	b.WriteString("**What I'm asking:** is this device supported, or what's missing for it to be?\n\n")

	failing := make([]protocol.DiagCommandStatus, 0, len(result.CommandChecks))
	for _, c := range result.CommandChecks {
		if !c.OK {
			failing = append(failing, c)
		}
	}
	if len(failing) == 0 {
		b.WriteString("All diagnostic checks passed.\n")
		return b.String()
	}

	fmt.Fprintf(&b, "**Failing checks (%d/%d):**\n\n", len(failing), len(result.CommandChecks))
	for _, c := range failing {
		fmt.Fprintf(&b, "- `%s` — %s (confidence=%s, experimental=%v)\n", c.Command, c.Detail, c.Confidence, c.IsExperimental)
	}

	return b.String()
}
