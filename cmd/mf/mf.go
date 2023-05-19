package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/robertlestak/mf/pkg/mf"
	log "github.com/sirupsen/logrus"
)

var (
	Version string = "dev"
	mfFlags        = flag.NewFlagSet("mf", flag.ExitOnError)
)

func init() {
	ll, err := log.ParseLevel(os.Getenv("LOG_LEVEL"))
	if err != nil {
		ll = log.ErrorLevel
	}
	log.SetLevel(ll)
}

func usage() {
	fmt.Println("Usage: mf [options] -- <command> [args...]")
	fmt.Println("Options:")
	mfFlags.PrintDefaults()
}

func main() {
	checkCmd := mfFlags.String("check", "", "check command to run. If the command exits with a non-zero exit code, the process will be frozen until the check command exits with a zero exit code")
	checkInterval := mfFlags.String("interval", "5s", "interval to run the check command")
	checkDelay := mfFlags.String("delay", "0s", "delay to run the check command")
	checkTimeout := mfFlags.String("timeout", "0s", "timeout for successive failures of the check command. default is to never timeout")
	logLevel := mfFlags.String("log", log.GetLevel().String(), "log level")
	pid := mfFlags.Int("pid", 0, "pid of process to monitor")
	version := mfFlags.Bool("version", false, "print version and exit")
	mfFlags.Usage = usage
	mfFlags.Parse(os.Args[1:])
	logLevelParsed, err := log.ParseLevel(*logLevel)
	if err != nil {
		logLevelParsed = log.ErrorLevel
	}
	log.SetLevel(logLevelParsed)
	if *version {
		fmt.Println(Version)
		os.Exit(0)
	}
	parsedInterval, err := time.ParseDuration(*checkInterval)
	if err != nil {
		log.WithError(err).Fatal("failed to parse check interval")
	}
	parsedDelay, err := time.ParseDuration(*checkDelay)
	if err != nil {
		log.WithError(err).Fatal("failed to parse check delay")
	}
	parsedTimeout, err := time.ParseDuration(*checkTimeout)
	if err != nil {
		log.WithError(err).Fatal("failed to parse check timeout")
	}
	p := &mf.Process{
		CheckCommand:  *checkCmd,
		CheckInterval: parsedInterval,
		CheckDelay:    parsedDelay,
		CheckTimeout:  parsedTimeout,
		Pid:           *pid,
	}
	if p.Pid == 0 {
		if len(mfFlags.Args()) < 1 {
			log.Fatal("no command or -pid provided")
		}
		cmdstr := strings.Join(mfFlags.Args(), " ")
		p.Command = cmdstr
		if err := p.Start(); err != nil {
			log.WithError(err).Error("failed to start process")
		}
		go p.Checker()
		if err := p.Wait(); err != nil {
			log.WithError(err).Error("failed to wait for process")
		}
	} else {
		if !p.PidExists() {
			log.Fatal("pid does not exist")
		}
		p.State = mf.Running
		p.Checker()
	}
}
