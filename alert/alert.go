// Package alert alert rules and templatize.
package alert

import (
	"bytes"
	"fmt"
	"text/template"
	"time"

	"github.com/Akagi201/utilgo/jobber"
	log "github.com/sirupsen/logrus"
	"github.com/tengattack/esalert/action"
	"github.com/tengattack/esalert/context"
	"github.com/tengattack/esalert/luautil"
	"github.com/tengattack/esalert/search"
	yaml "gopkg.in/yaml.v2"
)

// Alert encompasses a search query which will be run periodically, the results
// of which will be checked against a condition. If the condition returns true a
// set of actions will be performed
type Alert struct {
	Name        string            `yaml:"name"`
	Interval    string            `yaml:"interval"`
	SearchIndex string            `yaml:"search_index"`
	SearchType  string            `yaml:"search_type"`
	Search      search.Dict       `yaml:"search"`
	SearchQuery string            `yaml:"search_query"`
	Process     luautil.LuaRunner `yaml:"process"`

	Jobber         *jobber.FullTimeSpec
	SearchIndexTPL *template.Template
	SearchTypeTPL  *template.Template
	SearchTPL      *template.Template
}

func templatizeHelper(i interface{}, lastErr error) (*template.Template, error) {
	if lastErr != nil {
		return nil, lastErr
	}
	var str string
	if s, ok := i.(string); ok {
		str = s
	} else {
		b, err := yaml.Marshal(i)
		if err != nil {
			return nil, err
		}
		str = string(b)
	}

	return template.New("").Parse(str)
}

// Init initializes some internal data inside the Alert, and must be called
// after the Alert is unmarshaled from yaml (or otherwise created)
func (a *Alert) Init() error {
	var err error
	a.SearchIndexTPL, err = templatizeHelper(a.SearchIndex, err)
	a.SearchTypeTPL, err = templatizeHelper(a.SearchType, err)
	if a.Search == nil {
		a.SearchTPL, err = templatizeHelper(a.SearchQuery, err)
	} else {
		a.SearchTPL, err = templatizeHelper(&a.Search, err)
	}
	if err != nil {
		return err
	}

	jb, err := jobber.ParseFullTimeSpec(a.Interval)
	if err != nil {
		return fmt.Errorf("parsing interval: %s", err)
	}
	a.Jobber = jb

	return nil
}

func (a Alert) Run() {
	kv := log.Fields{
		"name": a.Name,
	}
	log.WithFields(kv).Infoln("running alert")

	now := time.Now()
	c := context.Context{
		Name:      a.Name,
		StartedTS: uint64(now.Unix()),
		Time:      now,
	}

	searchIndex, searchType, searchQuery, err := a.CreateSearch(c)
	if err != nil {
		kv["err"] = err
		log.WithFields(kv).Errorln("failed to create search data")
		return
	}

	log.WithFields(kv).Debugln("running search step")
	res, err := search.Search(searchIndex, searchType, searchQuery)
	if err != nil {
		kv["err"] = err
		log.WithFields(kv).Errorln("failed at search step")
		return
	}
	c.Result = res

	log.WithFields(kv).Debugln("running process step")
	processRes, ok := a.Process.Do(c)
	if !ok {
		log.WithFields(kv).Errorln("failed at process step")
		return
	}

	// if processRes isn't an []interface{}, actionsRaw will be the nil value of
	// []interface{}, which has a length of 0, so either way this works
	actionsRaw, _ := processRes.([]interface{})
	if len(actionsRaw) == 0 {
		log.WithFields(kv).Debugln("no actions returned")
	}

	actions := make([]action.Action, len(actionsRaw))
	for i := range actionsRaw {
		a, err := action.ToActioner(actionsRaw[i])
		if err != nil {
			kv["err"] = err
			log.WithFields(kv).Errorln("error unpacking action")
			return
		}
		actions[i] = a
	}

	for i := range actions {
		kv["action"] = actions[i].Type
		log.WithFields(kv).Infoln("performing action")
		if err := actions[i].Do(c); err != nil {
			kv["err"] = err
			log.WithFields(kv).Errorln("failed to complete action")
			return
		}
	}
}

func (a Alert) CreateSearch(c context.Context) (string, string, interface{}, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 1024))
	if err := a.SearchIndexTPL.Execute(buf, &c); err != nil {
		return "", "", nil, err
	}
	searchIndex := buf.String()

	buf.Reset()
	if err := a.SearchTypeTPL.Execute(buf, &c); err != nil {
		return "", "", nil, err
	}
	searchType := buf.String()

	buf.Reset()
	if err := a.SearchTPL.Execute(buf, &c); err != nil {
		return "", "", nil, err
	}
	searchRaw := buf.Bytes()

	var search search.Dict
	if err := yaml.Unmarshal(searchRaw, &search); err != nil {
		return "", "", nil, err
	}

	return searchIndex, searchType, search, nil
}
