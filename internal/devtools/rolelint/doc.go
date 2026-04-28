// Package rolelint defines Swobu's private naming checker and analyzer.
//
// It enforces naming laws that generic linters do not understand:
// weak concrete type names, weak file basenames, and direct affinity between a
// concrete struct name and the dominant repo-owned interface noun it is used as.
package rolelint
