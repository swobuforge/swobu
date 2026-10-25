package controlplane

// Protocol is the exact host-local CLI/daemon Control contract version.
// Version 11 moves operational shutdown/telemetry into canonical /api/v1
// and removes the old _swobu control namespace instead of aliasing it.
const Protocol = 11
