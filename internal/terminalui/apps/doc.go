// Package apps defines terminalui app-structure conventions.
//
// Each app under terminalui/apps/<appname> follows the same layout:
//   - app/state: reducer/model for presentation state
//   - app/views: declarative view assembly from state
//   - app/selectors (optional): read-model derivations
//   - run.go or presenter.go at app root: runtime entry adapter only
//
// This keeps app implementations parallel and predictable.
package apps
