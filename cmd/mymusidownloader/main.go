package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mymusidownloader/internal/config"
	"mymusidownloader/internal/downloader"
	"mymusidownloader/internal/track"
	"mymusidownloader/internal/web"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to config.json")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	store := track.NewFileStore(cfg.MetadataPath)
	svc, err := track.NewService(
		cfg.LibraryPath,
		cfg.MaxConcurrent,
		store,
		downloader.ShellDownloader{YTDLPPath: cfg.YTDLPPath},
		downloader.FFmpegMediaTool{FFmpegPath: cfg.FFmpegPath},
	)
	if err != nil {
		log.Fatalf("start service: %v", err)
	}

	server, err := web.New(svc)
	if err != nil {
		log.Fatalf("build web server: %v", err)
	}

	httpServer := &http.Server{
		Addr:              cfg.Address(),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}()

	log.Printf("listening on http://%s", cfg.Address())
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}
