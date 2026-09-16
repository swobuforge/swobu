// Package cockpit implements Swobu's terminal operator interface.
//
// Run loads the daemon-backed read model and enters the interactive application
// when both streams are terminals. Otherwise it writes a rendered snapshot.
// Local effect feedback remains with its originating row, section, or feature.
// The root shell owns only the refresh warning left after deletion removes the
// originating workspace. Daemon lifecycle and provider requests remain outside
// this package.
package cockpit
