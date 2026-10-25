// Package tariff reads exact provider-local economic versions from the accepted
// Models SQLite catalogue. It does not choose a route, canonical Model,
// comparison representative, tariff or token forecast on the caller's behalf.
// Selection requires the exact provider selector/version and semantic tariff
// name. Historical version selection precedes input-band selection; one complete
// vector applies to the whole request. Currency and optional rates remain
// explicit. Only USD ordinary input/output costing is supported here; unknown
// quantities, optional billable dimensions and FX need their own proven policy.
// Runtime admission retains this version identity and declares quantity
// coverage through Accounting Quote. Reader acquisition alone does not qualify
// request forecasting, supported billable dimensions or provider invoice truth.
// Read-only SQLite file URIs preserve native path identity, including Windows
// drive letters, without interpreting a drive as a URI authority.
package tariff
