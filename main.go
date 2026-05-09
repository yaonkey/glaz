package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfgPath := flag.String("config", "glaz.json", "Путь до JSON-конфига с директориями логов")
	date := flag.String("date", time.Now().Format("2006-01-02"), "Дата логов в формате YYYY-MM-DD")
	flag.Parse()

	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка чтения конфига: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	coordinator, err := NewCoordinator(ctx, cfg, *date)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка инициализации мониторинга: %v\n", err)
		os.Exit(1)
	}
	defer coordinator.Close()

	if err := RunTUI(ctx, coordinator); err != nil {
		fmt.Fprintf(os.Stderr, "ошибка TUI: %v\n", err)
		os.Exit(1)
	}
}
