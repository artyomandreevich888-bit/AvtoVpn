//go:build windows

package main

import (
	"fmt"
	"os/exec"
)

func sendNotification(title, message string) {
	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$n = New-Object System.Windows.Forms.NotifyIcon
$n.Icon = [System.Drawing.SystemIcons]::Application
$n.Visible = $true
$n.BalloonTipTitle = '%s'
$n.BalloonTipText = '%s'
$n.ShowBalloonTip(4000)
Start-Sleep -Milliseconds 4500
$n.Dispose()
`, title, message)
	exec.Command("powershell", "-WindowStyle", "Hidden", "-NonInteractive", "-Command", script).Start()
}
