package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/taybart/log"
	"github.com/taybart/run"
)

func usage() {
	fmt.Printf("%sUsage: run [-w exts] [--] <script-to-run> [args...]%s\n", log.Red, log.Rtd)
	fmt.Println("  -w exts   restart when files with these extensions change (ex. -w mn,rest,css, or -w '*' for any file)")
}

func main() {
	log.SetPlain()

	// flags stop at the script (or --), so its own flags are passed through
	flags := flag.NewFlagSet("run", flag.ExitOnError)
	flags.Usage = usage
	watchFlag := flags.String("w", "", "")
	flags.Parse(os.Args[1:])

	if flags.NArg() < 1 {
		usage()
		os.Exit(1)
	}
	script := flags.Arg(0)
	scriptArgs := flags.Args()[1:]
	exts := run.ParseExts(*watchFlag)

	log.Infof("%sRunning %s...%s\n", log.Blue, script, log.Rtd)
	if len(exts) > 0 {
		log.Infof("%sWatching for changes to: %s%s\n", log.Blue, strings.Join(exts, ", "), log.Rtd)
	}
	log.Infof("%sPress 'r' to reload, 'q' to quit%s\n", log.Yellow, log.Rtd)

	run.Do(script, scriptArgs, true, exts)
}
