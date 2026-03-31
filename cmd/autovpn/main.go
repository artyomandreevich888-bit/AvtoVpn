package main

import (
	"context"

	"github.com/mewmewmemw/autovpn/frontend"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func main() {
	app := NewApp()

	appMenu := menu.NewMenu()
	trayMenu := appMenu.AddSubmenu("AutoVPN")
	trayMenu.AddText("Открыть", keys.CmdOrCtrl("o"), func(_ *menu.CallbackData) {
		wailsRuntime.WindowShow(app.ctx)
	})
	trayMenu.AddSeparator()
	trayMenu.AddText("Подключить", nil, func(_ *menu.CallbackData) {
		if !app.manager.Engine.IsRunning() {
			go app.manager.Connect()
		}
	})
	trayMenu.AddText("Отключить", nil, func(_ *menu.CallbackData) {
		if app.manager.Engine.IsRunning() {
			app.manager.Disconnect()
		}
	})
	trayMenu.AddSeparator()
	trayMenu.AddText("Выход", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) {
		wailsRuntime.Quit(app.ctx)
	})

	err := wails.Run(&options.App{
		Title:     "AutoVPN",
		Width:     360,
		Height:    600,
		MinWidth:  360,
		MinHeight: 540,
		AssetServer: &assetserver.Options{
			Assets: frontend.Assets,
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
		// When user clicks the window close button: hide to tray instead of quitting
		OnBeforeClose: func(ctx context.Context) bool {
			wailsRuntime.WindowHide(app.ctx)
			return true // returning true cancels the close
		},
		Menu:             appMenu,
		BackgroundColour: &options.RGBA{R: 14, G: 14, B: 22, A: 255},
	})
	if err != nil {
		panic(err)
	}
}
