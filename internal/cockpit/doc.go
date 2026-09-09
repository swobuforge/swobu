// Package cockpit implements Swobu's terminal operator interface.
//
// Run loads the daemon-backed read model and enters the interactive application
// when both streams are terminals. Otherwise it writes a rendered snapshot.
// Daemon lifecycle and provider requests remain outside this package.
package cockpit
