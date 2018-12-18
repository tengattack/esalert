package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/tengattack/esalert/alert"
	"github.com/tengattack/esalert/config"
	"github.com/tengattack/tgo/log"
	yaml "gopkg.in/yaml.v2"
)

var (
	// Version of esalert
	Version = "1.0.0-dev"
)

func main() {
	log.LogAccess.Infof("starting esalert %s", Version)
	fstat, err := os.Stat(config.Opts.AlertFileDir)
	if err != nil {
		log.LogError.WithFields(logrus.Fields{
			"err": err,
		}).Fatalln("failed getting alert definitions")
	}

	files := make([]string, 0, 10)
	if !fstat.IsDir() {
		files = append(files, config.Opts.AlertFileDir)
	} else {
		fileInfos, err := ioutil.ReadDir(config.Opts.AlertFileDir)
		if err != nil {
			log.LogError.WithFields(logrus.Fields{
				"err": err,
			}).Fatalln("failed getting alert dir info")
		}
		for _, fi := range fileInfos {
			if !fi.IsDir() {
				files = append(files, filepath.Join(config.Opts.AlertFileDir, fi.Name()))
			}
		}
	}

	for _, file := range files {
		kv := logrus.Fields{
			"file": file,
		}
		var alerts []alert.Alert
		b, err := ioutil.ReadFile(file)
		if err != nil {
			kv["err"] = err
			log.LogError.WithFields(kv).Fatalln("failed to read alert config")
		}

		if err := yaml.Unmarshal(b, &alerts); err != nil {
			kv["err"] = err
			log.LogError.WithFields(kv).Fatalln("failed to parse yaml")
		}

		for i := range alerts {
			kv["name"] = alerts[i].Name
			log.LogAccess.WithFields(kv).Infoln("initializing alert")
			if err := alerts[i].Init(); err != nil {
				kv["err"] = err
				log.LogError.WithFields(kv).Fatalln("failed to initialize alert")
			}

			if config.Opts.ForceRun != "" && config.Opts.ForceRun == alerts[i].Name {
				alerts[i].Run()
				time.Sleep(250 * time.Millisecond) // allow time for logs to print
				return
			} else if config.Opts.ForceRun == "" {
				go alertSpin(alerts[i])
			}
		}
	}

	// If we made it this far with --force-run set to something it means an
	// alert by that name was never found, so we should error
	if config.Opts.ForceRun != "" {
		log.LogError.Errorln("could not find alert with name given by --force-run")
		os.Exit(1)
	}

	select {}
}

func alertSpin(a alert.Alert) {
	for {
		now := time.Now()
		next := a.Jobber.Next(now)
		if now == next {
			go a.Run()
			time.Sleep(time.Second)
		} else {
			time.Sleep(next.Sub(now))
		}
	}
}
