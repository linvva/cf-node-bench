package main

import (
	"embed"
	"log"

	"github.com/linvva/cf-node-bench/internal/storage"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	store, err := storage.OpenDefault()
	if err != nil {
		log.Fatal(err)
	}
	app := NewApp(store)
	err = wails.Run(&options.App{
		Title: "CF Node Bench", Width: 1440, Height: 900, MinWidth: 1180, MinHeight: 720,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 246, G: 247, B: 249, A: 1},
		OnStartup:        app.startup,
		Bind:             []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
