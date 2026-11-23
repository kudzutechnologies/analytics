package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	bs "github.com/kudzutechnologies/analytics/forwarder-bs"
	log "github.com/sirupsen/logrus"
	"os"
	"os/signal"
	"syscall"
)

var MajorVersion = "dev"

func main() { os.Exit(runMain()) }
func runMain() int {
	c, e := bs.ParseConfigFromEnv()
	if errors.Is(e, flag.ErrHelp) {
		fmt.Println("Kudzu Basic Station: --config FILE --bootstrap-gw EUI --bootstrap-dir DIR; see README.md")
		return 0
	}
	if e != nil {
		log.Error(e)
		return 1
	}
	if c.ShowVersion {
		fmt.Printf("Kudzu Basic Station v%s (Git %s)\n", MajorVersion, bs.Version())
		return 0
	}
	if c.BootstrapGW != "" {
		if e = bs.ExportGatewayBootstrap(c); e != nil {
			log.Error(e)
			return 1
		}
		return 0
	}
	if c.PairPin != "" {
		s, e := bs.RenderPairConfig(c.PairPin, c)
		if e != nil {
			log.Error(e)
			return 1
		}
		if c.WriteConfig {
			if c.ConfigPath == "" {
				log.Error("--write requires --config")
				return 1
			}
			if e = os.WriteFile(c.ConfigPath, []byte(s), 0600); e != nil {
				log.Error(e)
				return 1
			}
			os.Chmod(c.ConfigPath, 0600)
		} else {
			fmt.Print(s)
		}
		return 0
	}
	if c.Verbose {
		c.LogLevel = "debug"
	}
	level, _ := log.ParseLevel(c.LogLevel)
	log.SetLevel(level)
	if c.LogFile != "" {
		f, e := os.OpenFile(c.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			log.Error(e)
			return 1
		}
		defer f.Close()
		log.SetOutput(f)
	}
	f, e := bs.New(c, bs.Dependencies{})
	if e != nil {
		log.Error(e)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e = f.Run(ctx); e != nil {
		log.Error(e)
		return 1
	}
	return 0
}
