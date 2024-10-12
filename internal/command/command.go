package command

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/bento01dev/das/internal/config"
	"github.com/bento01dev/das/internal/controller"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Run() error {
	initLog()
	slog.Info("das says hi..")
	var configFilePath string
	flag.StringVar(&configFilePath, "config_file", "config.json", "config file path")
	flag.Parse()
	slog.Info("reading config", "config_path", configFilePath)
	conf, err := config.Parse(configFilePath)
	if err != nil {
		slog.Error("error parsing config", "config_path", configFilePath, "err", err.Error())
		return err
	}
	err = controller.Start(conf)
	if err != nil {
		slog.Error("error in starting manager", "config_path", configFilePath, "err", err.Error())
		return fmt.Errorf("error in manager after reading config from %s: %w", configFilePath, err)
	}
	return nil
}

func initLog() {
	logLevelStr := os.Getenv("LOG_LEVEL")
	fmt.Println("log level in env:", logLevelStr)
	var logLevel slog.Level = slog.LevelWarn
	if logLevelStr != "" {
		switch strings.ToLower(logLevelStr) {
		case "debug":
			logLevel = slog.LevelDebug
		case "info":
			logLevel = slog.LevelInfo
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		}
	}
	opts := slog.HandlerOptions{
		Level: logLevel,
	}

	logHandler := slog.NewJSONHandler(os.Stdout, &opts)

	logger := slog.New(logHandler)
	slog.SetDefault(logger)
	log.SetLogger(logr.FromSlogHandler(logHandler))
}
