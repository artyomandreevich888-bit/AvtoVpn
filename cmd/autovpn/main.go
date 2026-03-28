package main

import (
	"github.com/mewmewmemw/autovpn/frontend"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "AutoVPN",
		Width:     360,
		Height:    480,
		MinWidth:  360,
		MinHeight: 480,
		AssetServer: &assetserver.Options{
			Assets: frontend.Assets,
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		panic(err)
	}
}
