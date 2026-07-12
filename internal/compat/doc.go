// Package compat owns the shared compatibility vocabulary for feature support,
// support decisions, and route capability lookup.
//
// It centralizes the semantic terms that previously drifted across request
// features and route capability helpers so provider dispatch can ask one
// question: what feature does this route support, and at what level?
package compat
