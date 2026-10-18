package main

import (
	"fmt"
	"os"

	"github.com/taybart/log"
	"github.com/taybart/run"
)

func main() {
	log.SetPlain()
	if len(os.Args) < 2 {
		fmt.Printf("%sUsage: run <script-to-run> [args...]%s\n", log.Red, log.Rtd)
		os.Exit(1)
	}

	script := os.Args[1]
	scriptArgs := os.Args[2:]

	log.Infof("%sRunning %s...%s\n", log.Blue, script, log.Rtd)
	log.Infof("%sPress 'r' to reload, 'q' to quit%s\n", log.Yellow, log.Rtd)

	run.Do(script, scriptArgs, true)
}
