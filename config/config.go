// Package config parses command-line/environment/config file arguments
// and make available to other packages.
package config

import (
	"io/ioutil"
	"path"
	"runtime"

	"gopkg.in/yaml.v2"

	"github.com/Akagi201/utilgo/conflag"
	flags "github.com/jessevdk/go-flags"
	"github.com/tengattack/tgo/log"
)

// Opts configs
var Opts struct {
	Conf              string     `long:"conf" description:"esalert config file"`
	AlertFileDir      string     `yaml:"alerts" long:"alerts" short:"a" required:"true" description:"A yaml file, or directory with yaml files, containing alert definitions"`
	ElasticSearchAddr string     `yaml:"es-addr" long:"es-addr" default:"127.0.0.1:9200" description:"Address to find an elasticsearch instance on"`
	ElasticSearchUser string     `yaml:"es-user" long:"es-user" default:"elastic" description:"Username for the elasticsearch"`
	ElasticSearchPass string     `yaml:"es-pass" long:"es-pass" default:"changeme" description:"Password for the elasticsearch"`
	LuaInit           string     `yaml:"lua-init" long:"lua-init" description:"If set the given lua script file will be executed at the initialization of every lua vm"`
	LuaVMs            int        `yaml:"lua-vms" long:"lua-vms" default:"1" description:"How many lua vms should be used. Each vm is completely independent of the other, and requests are executed on whatever vm is available at that moment. Allows lua scripts to not all be blocked on the same os thread"`
	SlackWebhook      string     `yaml:"slack-webhook" long:"slack-webhook" description:"Slack webhook url, required if using any Slack actions"`
	ForceRun          string     `yaml:"force-run" long:"force-run" description:"If set with the name of an alert, will immediately run that alert and exit. Useful for testing changes to alert definitions"`
	Log               log.Config `yaml:"log" long:"log" description:"logging options"`
}

func init() {
	runtime.GOMAXPROCS(runtime.NumCPU())
}

func init() {
	parser := flags.NewParser(&Opts, flags.Default|flags.IgnoreUnknown)

	parser.Parse()

	if Opts.Conf != "" {
		switch path.Ext(Opts.Conf) {
		case ".yaml", ".yml":
			f, err := ioutil.ReadFile(Opts.Conf)
			if err != nil {
				panic(err)
			}
			err = yaml.Unmarshal(f, &Opts)
			if err != nil {
				panic(err)
			}
		default:
			conflag.LongHyphen = true
			conflag.BoolValue = false
			args, err := conflag.ArgsFrom(Opts.Conf)
			if err != nil {
				panic(err)
			}

			parser.ParseArgs(args)
		}
	}

	if Opts.Log.AccessLevel == "" || Opts.Log.ErrorLevel == "" {
		// Load default logging configuration
		Opts.Log = *log.DefaultConfig
	}

	err := log.InitLog(&Opts.Log)
	if err != nil {
		panic(err)
	}

	log.LogAccess.Debugf("esalert opts: %+v", Opts)
}
