package main

import (
	"os"

	"github.com/bento01dev/das/internal/command"
)

func main() {
	if err := command.Run(); err != nil {
		os.Exit(1)
	}
}
