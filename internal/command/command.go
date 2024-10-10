package command

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

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

type ctrlLogger struct {
	logger *slog.Logger
}

func (cl *ctrlLogger) Init(info logr.RuntimeInfo) {
}

func (cl *ctrlLogger) Enabled(level int) bool {
	return true
}

func (cl *ctrlLogger) Info(level int, msg string, keysAndValues ...any) {
	cl.logger.Info(msg, keysAndValues...)
}

func (cl *ctrlLogger) Error(err error, msg string, keysAndValues ...any) {
	keysAndValues = append(keysAndValues, "err")
	keysAndValues = append(keysAndValues, err.Error())
	cl.logger.Error(msg, keysAndValues...)
}

func (cl *ctrlLogger) WithValues(keysAndValues ...any) logr.LogSink {
	return cl
}

func (cl *ctrlLogger) WithName(name string) logr.LogSink {
	return cl
}

func initLog() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	log.SetLogger(logr.New(&ctrlLogger{logger: logger}))
}
