// Package protocolsurface owns duplex protocol-family codec composition shared
// by adapter edges.
//
// Inbound and outbound edges depend on this package so family codec selection
// does not create cross-imports between ingress and egress adapter trees.
package protocolsurface
