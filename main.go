package main

import (
	"embed"
	"log"
	"path/filepath"

	"github.com/linvva/cf-node-bench/internal/storage"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	updater.HandleHelperMode()
	appDataDir, err := storage.DefaultDir()
	if err != nil {
		log.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(appDataDir, "data.json"))
	if err != nil {
		log.Fatal(err)
	}
	app := NewApp(store)
	desktop := application.New(application.Options{
		Name:     "CF Node Bench",
		Services: []application.Service{application.NewService(app)},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Windows: application.WindowsOptions{WebviewUserDataPath: filepath.Join(appDataDir, "WebView2")},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	app.desktop = desktop
	if err := app.configureUpdater(desktop); err != nil {
		log.Fatal(err)
	}
	window := desktop.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "CF Node Bench", Width: 1440, Height: 900, MinWidth: 1180, MinHeight: 720,
		BackgroundColour: application.NewRGB(246, 247, 249),
		EnableFileDrop:   true,
		URL:              "/",
	})
	window.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		desktop.Event.Emit("files:dropped", event.Context().DroppedFiles())
	})
	if err := desktop.Run(); err != nil {
		log.Fatal(err)
	}
}
