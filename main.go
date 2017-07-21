package main

import (
	"log"
	"os"

	"github.com/otiai10/yacle/commands"
	"github.com/urfave/cli"
)

func main() {
	app := cli.NewApp()
	app.Name = "yacle"
	app.Usage = "Yet Another CWL Engine"
	app.Commands = []cli.Command{
		commands.Run,
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatalln(err)
	}
}
