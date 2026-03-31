//go:build !windows

package main

// SetAutoStart is a no-op on non-Windows platforms.
func (a *App) SetAutoStart(enabled bool) string {
	return ""
}

// GetAutoStart always returns false on non-Windows platforms.
func (a *App) GetAutoStart() bool {
	return false
}
