//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const autoStartRegKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const autoStartAppName = "AutoVPN"

// SetAutoStart enables or disables Windows autostart via registry.
func (a *App) SetAutoStart(enabled bool) string {
	k, err := registry.OpenKey(registry.CURRENT_USER, autoStartRegKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Sprintf("registry open: %v", err)
	}
	defer k.Close()

	if enabled {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Sprintf("get exe path: %v", err)
		}
		if err := k.SetStringValue(autoStartAppName, exePath); err != nil {
			return fmt.Sprintf("registry set: %v", err)
		}
	} else {
		if err := k.DeleteValue(autoStartAppName); err != nil && err != registry.ErrNotExist {
			return fmt.Sprintf("registry delete: %v", err)
		}
	}
	return ""
}

// GetAutoStart returns true if autostart is enabled.
func (a *App) GetAutoStart() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, autoStartRegKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(autoStartAppName)
	return err == nil
}
