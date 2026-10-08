package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	_ "time/tzdata"

	"weatherbot/internal/app"
	appconfig "weatherbot/internal/config"
	"weatherbot/internal/dashboard"
	"weatherbot/internal/execution"
	"weatherbot/internal/research"
	"weatherbot/internal/store"
)

func main() {
	configPath := flag.String("config", "config.json", "configuration file")
	flag.Parse()
	command := "once"
	if flag.NArg() > 0 {
		command = flag.Arg(0)
	}
	if command == "run" || command == "once" {
		lock, err := net.Listen("tcp", "localhost:8788")
		if err != nil {
			log.Fatal("another collector is running, or local lock port 8788 is occupied")
		}
		defer lock.Close()
	}
	cfg, err := appconfig.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	stations, err := appconfig.LoadStations(cfg.StationsFile)
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.New(cfg.DataDirectory)
	if err != nil {
		log.Fatal(err)
	}
	bot := app.New(cfg, stations, st)
	var researchServer *research.Server
	if cfg.ResearchStationsFile != "" {
		researchStations, loadErr := appconfig.LoadStationList(cfg.ResearchStationsFile)
		if loadErr != nil {
			log.Fatal(loadErr)
		}
		researchServer = research.New(researchStations, "data/weather-research")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	switch command {
	case "reset-paper":
		backup, err := execution.Reset(filepath.Join(cfg.DataDirectory, "paper-ledger.json"), cfg)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("纸面账户已重置为 $%.2f；旧账本备份：%s\n", cfg.BankrollDollars, backup)
	case "once":
		snapshot, err := bot.RunOnce(ctx)
		if err != nil {
			log.Fatal(err)
		}
		ready, waiting, paperBuys := 0, 0, 0
		for _, report := range snapshot.Reports {
			if report.Status == "READY" {
				ready++
			}
			if report.Status == "WAITING" {
				waiting++
			}
			for _, signal := range report.Signals {
				if signal.Action == "PAPER_FILLED" {
					paperBuys++
				}
			}
		}
		fmt.Printf("扫描完成：%d 个最高温市场，%d 个已定价，%d 个等待数据，%d 个模拟买入信号。详情：%s/latest.json\n", len(snapshot.Reports), ready, waiting, paperBuys, cfg.DataDirectory)
	case "run":
		if researchServer != nil {
			go researchServer.Run(ctx)
		}
		go func() {
			log.Printf("dashboard: http://%s", cfg.ListenAddress)
			var researchHandler http.Handler
			if researchServer != nil {
				researchHandler = researchServer.Handler()
			}
			if err := dashboard.New(cfg.ListenAddress, st, researchHandler).ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("dashboard failed: %v", err)
			}
		}()
		if err := bot.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Fatal(err)
		}
	case "serve":
		log.Printf("dashboard: http://%s", cfg.ListenAddress)
		log.Fatal(dashboard.New(cfg.ListenAddress, st, nil).ListenAndServe())
	default:
		log.Fatalf("unknown command %q; use once, run, serve, or reset-paper", command)
	}
}
