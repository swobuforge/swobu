package producttelemetry

// firstRunNoticeText is the single non-blocking telemetry notice, printed once on
// first daemon start. It states what is collected, what is never collected, and
// how to disable. The wording is honest at every phase: it makes no claim
// stronger than the implementation proves and never calls the payload
// "anonymous" — a pseudonymous installation id is part of the program (see
// product-telemetry.md). The daemon starts regardless of notice state; this is
// information, not a gate. Printing is the CLI's concern; the store only claims
// the once-marker atomically (ClaimNotice).
const firstRunNoticeText = "Swobu sends privacy-minimized usage and reliability telemetry tied to a random installation id (not your account or machine). It never sends prompts, responses, credentials, endpoints, or user-defined names. Turn off: swobu telemetry off or DO_NOT_TRACK."

func FirstRunNoticeText() string {
	return firstRunNoticeText
}
