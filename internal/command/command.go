package command

import (
	"flag"
	"fmt"

	"github.com/bento01dev/das/internal/config"
	"github.com/bento01dev/das/internal/controller"
)

func Run() error {
	fmt.Println("das says hi..")
	var configFilePath string
	flag.StringVar(&configFilePath, "config_file", "config.json", "config file path")
	flag.Parse()
	fmt.Println("Reading config from:", configFilePath)
	conf, err := config.Parse(configFilePath)
	if err != nil {
		return err
	}
	err = controller.Start(conf)
	if err != nil {
		return fmt.Errorf("error in manager after reading config from %s: %w", configFilePath, err)
	}
	return nil
}
