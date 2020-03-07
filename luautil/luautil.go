// Package luautil embeds Lua VM into Go
package luautil

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/nubix-io/gluasocket"
	"github.com/cjoudrey/gluahttp"
	"github.com/sirupsen/logrus"
	"github.com/tengattack/esalert/config"
	"github.com/tengattack/esalert/context"
	"github.com/tengattack/gluasql"
	"github.com/tengattack/tgo/log"
	lua "github.com/yuin/gopher-lua"
	gluajson "layeh.com/gopher-json"
)

// LuaRunner performs some arbitrary lua code. The code can either be sourced from a
// file or from a raw string (Inline).
type LuaRunner struct {
	File   string `yaml:"lua_file"`
	Inline string `yaml:"lua_inline"`
}

// Do performs the actual lua code, returning whatever the lua code returned, or
// false if there was an error
func (l *LuaRunner) Do(c context.Context) (interface{}, bool) {
	if l.File != "" {
		return RunFile(c, l.File)
	} else if l.Inline != "" {
		return RunInline(c, l.Inline)
	}
	return nil, false
}

type cmd struct {
	ctx      context.Context
	filename string
	inline   string
	retCh    chan interface{}
}

var cmdCh = make(chan cmd)

// RunInline takes the given lua code, and runs it with the given ctx variable
// set as the lua global variable "ctx". The lua code is expected to return a
// boolean value, which is passed back as the first boolean return. The second
// boolean return will be false if there was an error and the code wasn't run
func RunInline(ctx context.Context, code string) (interface{}, bool) {
	c := cmd{
		ctx:    ctx,
		inline: code,
		retCh:  make(chan interface{}),
	}
	cmdCh <- c
	ret, ok := <-c.retCh
	return ret, ok
}

// RunFile is similar to RunInline, except it takes in a filename which has the
// lua code to run. Note that the file's contents are cached, so the file is
// only opened and read the first time it's used.
func RunFile(ctx context.Context, filename string) (interface{}, bool) {
	c := cmd{
		ctx:      ctx,
		filename: filename,
		retCh:    make(chan interface{}),
	}
	cmdCh <- c
	ret, ok := <-c.retCh
	return ret, ok
}

type runner struct {
	id int // solely used to tell lua vms apart in logs
	l  *lua.LState

	// Set of files and inline functions hashes already in the global namespace
	m map[string]bool
}

func init() {
	for i := 0; i < config.Opts.LuaVMs; i++ {
		newRunner(i)
	}
}

func newRunner(i int) {
	l := lua.NewState()

	// Preload modules
	gluasocket.Preload(l)
	gluasql.Preload(l)
	l.PreloadModule("http", gluahttp.NewHttpModule(&http.Client{}).Loader)
	gluajson.Preload(l)

	r := runner{
		id: i,
		l:  l,
		m:  map[string]bool{},
	}
	go r.spin()
}

func shortInline(code string) string {
	if len(code) > 20 {
		return code[:20] + " ..."
	}
	return code
}

func (r *runner) spin() {
	kv := logrus.Fields{
		"runnerID": r.id,
	}
	log.LogAccess.WithFields(kv).Debugln("initializing lua vm")

	if config.Opts.LuaInit != "" {
		initKV := logrus.Fields{
			"runnerID": r.id,
			"filename": config.Opts.LuaInit,
		}
		initFnName, err := r.loadFile(config.Opts.LuaInit)
		if err != nil {
			initKV["err"] = err
			log.LogError.WithFields(initKV).Fatalln("error initializing lua vm")
		} else {
			if err = r.l.CallByParam(lua.P{
				Fn:      r.l.GetGlobal(initFnName),
				NRet:    0,
				Protect: false,
			}); err != nil {
				initKV["err"] = err
				log.LogError.WithFields(initKV).Fatalln("error initializing lua vm")
			}
		}
	}

	for c := range cmdCh {
		var fnName string
		var err error
		if c.filename != "" {
			kv["filename"] = c.filename
			fnName, err = r.loadFile(c.filename)
		} else if c.inline != "" {
			kv["inline"] = shortInline(c.inline)
			fnName, err = r.loadInline(c.inline)
		}
		if err != nil {
			kv["err"] = err
			log.LogError.WithFields(kv).Errorln("error loading lua")
			close(c.retCh)
			continue
		}

		kv["fnName"] = fnName
		log.LogAccess.WithFields(kv).Debugln("executing lua")

		val := pushArbitraryValue(r.l, c.ctx) // push ctx onto the stack
		r.l.SetGlobal("ctx", val)             // set global variable "ctx" to ctx, pops it from stack
		// push function onto stack
		// call function, pops function from stack, pushes return
		if err = r.l.CallByParam(lua.P{
			Fn:      r.l.GetGlobal(fnName),
			NRet:    1,
			Protect: false,
		}); err != nil {
			log.LogError.WithFields(kv).Fatalln("error executing lua")
		}
		c.retCh <- PullArbitraryValue(r.l, true) // send back the function return, also popping it
		// stack is now clean
	}
}

func (r *runner) loadFile(name string) (string, error) {
	key := quickSha(name)
	if r.m[key] {
		return key, nil
	}

	log.LogAccess.WithFields(logrus.Fields{
		"runnerID": r.id,
		"filename": name,
		"fnName":   key,
	}).Debugln("loading lua file")
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var fn *lua.LFunction
	if fn, err = r.l.Load(f, name); err != nil {
		return "", err
	}
	r.l.SetGlobal(key, fn)

	r.m[key] = true
	return key, nil
}

func (r *runner) loadInline(code string) (string, error) {
	key := quickSha(code)
	if r.m[key] {
		return key, nil
	}

	log.LogAccess.WithFields(logrus.Fields{
		"runnerID": r.id,
		"inline":   shortInline(code),
		"fnName":   key,
	}).Debugln("loading lua inline")

	fn, err := r.l.Load(bytes.NewBufferString(code), key)
	if err != nil {
		return "", err
	}
	r.l.SetGlobal(key, fn)

	r.m[key] = true
	return key, nil
}

func quickSha(s string) string {
	sh := sha1.New()
	sh.Write([]byte(s))
	return hex.EncodeToString(sh.Sum(nil))
}

func PullArbitraryValue(l *lua.LState, remove bool) interface{} {
	if remove {
		defer l.Remove(-1)
	}
	return pullArbitraryValueInner(l, l.Get(-1))
}

func pullArbitraryValueInner(l *lua.LState, v lua.LValue) interface{} {
	switch t := v.Type(); t {
	case lua.LTNil:
		return nil
	case lua.LTBool:
		return lua.LVAsBool(v)
	case lua.LTNumber:
		f := lua.LVAsNumber(v)
		if float64(f) == float64(int(f)) {
			return int(f)
		}
		return float64(f)
	case lua.LTString:
		return lua.LVAsString(v)
	case lua.LTTable:
		m := map[string]interface{}{}
		tb := v.(*lua.LTable)
		arrSize := 0
		tb.ForEach(func(k, val lua.LValue) {
			key := pullArbitraryValueInner(l, k)
			if keyi, ok := key.(int); ok {
				if arrSize >= 0 && arrSize < keyi {
					arrSize = keyi
				}
				key = strconv.Itoa(keyi)
			} else {
				arrSize = -1
			}
			m[key.(string)] = pullArbitraryValueInner(l, val)
		})

		if arrSize >= 0 {
			ms := make([]interface{}, arrSize)
			for i := 0; i < arrSize; i++ {
				ms[i] = m[strconv.Itoa(i+1)]
			}
			return ms
		}

		return m
	default:
		panic(fmt.Sprintf("unknown lua type: %s", t))
	}
}

func pushArbitraryValue(l *lua.LState, i interface{}) lua.LValue {
	if i == nil {
		return lua.LNil
	}

	switch ii := i.(type) {
	case bool:
		return lua.LBool(ii)
	case int:
		return lua.LNumber(ii)
	case int8:
		return lua.LNumber(ii)
	case int16:
		return lua.LNumber(ii)
	case int32:
		return lua.LNumber(ii)
	case int64:
		return lua.LNumber(ii)
	case uint:
		return lua.LNumber(ii)
	case uint8:
		return lua.LNumber(ii)
	case uint16:
		return lua.LNumber(ii)
	case uint32:
		return lua.LNumber(ii)
	case uint64:
		return lua.LNumber(ii)
	case float64:
		return lua.LNumber(ii)
	case float32:
		return lua.LNumber(ii)
	case string:
		return lua.LString(ii)
	case []byte:
		return lua.LString(ii)
	default:
		v := reflect.ValueOf(i)
		switch v.Kind() {
		case reflect.Ptr:
			return pushArbitraryValue(l, v.Elem().Interface())

		case reflect.Struct:
			return PushTableFromStruct(l, v)

		case reflect.Map:
			return PushTableFromMap(l, v)

		case reflect.Slice:
			return PushTableFromSlice(l, v)

		default:
			panic(fmt.Sprintf("unknown type being pushed onto lua stack: %T %+v", i, i))
		}
	}
}

func PushTableFromStruct(l *lua.LState, v reflect.Value) lua.LValue {
	tb := l.NewTable()
	return pushTableFromStructInner(l, tb, v)
}

func pushTableFromStructInner(l *lua.LState, tb *lua.LTable, v reflect.Value) lua.LValue {
	t := v.Type()
	for j := 0; j < v.NumField(); j++ {
		var inline bool
		name := t.Field(j).Name
		if tag := t.Field(j).Tag.Get("luautil"); tag != "" {
			tagParts := strings.Split(tag, ",")
			if tagParts[0] == "-" {
				continue
			} else if tagParts[0] != "" {
				name = tagParts[0]
			}
			if len(tagParts) > 1 && tagParts[1] == "inline" {
				inline = true
			}
		}
		if inline {
			pushTableFromStructInner(l, tb, v.Field(j))
		} else {
			tb.RawSetString(name, pushArbitraryValue(l, v.Field(j).Interface()))
		}
	}
	return tb
}

func PushTableFromMap(l *lua.LState, v reflect.Value) lua.LValue {
	tb := l.NewTable()
	for _, k := range v.MapKeys() {
		tb.RawSet(pushArbitraryValue(l, k.Interface()),
			pushArbitraryValue(l, v.MapIndex(k).Interface()))
	}
	return tb
}

func PushTableFromSlice(l *lua.LState, v reflect.Value) lua.LValue {
	tb := l.NewTable()
	for j := 0; j < v.Len(); j++ {
		tb.RawSetInt(j+1, // because lua is 1-indexed
			pushArbitraryValue(l, v.Index(j).Interface()))
	}
	return tb
}
