// create language files from environment
package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"strings"

	"git.defalsify.org/vise.git/logging"
	"git.defalsify.org/vise.git/lang"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/common/env"
)

const (
	
	changeHeadSrc = `LOAD reset_account_authorized 0
LOAD reset_incorrect 0
CATCH incorrect_pin flag_incorrect_pin 1
CATCH pin_entry flag_account_authorized 0
` 

	selectSrc = `LOAD set_language 6
RELOAD set_language
CATCH terms flag_account_created 0
MOVE language_changed
`
)

var (
	logg = logging.NewVanilla()
	mouts string
	incmps string
)

func init() {
	env.LoadEnvVariables()
}

func toLanguageLabel(ln lang.Language) string {
	s := ln.Name
	v := strings.Split(s, " (")
	if len(v) > 1 {
		s = v[0]
	}
	return s
}

func toLanguageKey(ln lang.Language) string {
	s := toLanguageLabel(ln)
	return strings.ToLower(s)
}

func main() {
	var srcDir string

	flag.StringVar(&srcDir, "o", ".", "resource dir write to")
	flag.Parse()

	logg.Infof("start command", "dir", srcDir)

	err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load error: %v", err)
		os.Exit(1)
	}
	logg.Tracef("using languages", "lang", config.Languages)

	for i, v := range(config.Languages) {
		ln, err := lang.LanguageFromCode(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error parsing language: %s\n", v)
			os.Exit(1)
		}
		n := i + 1
		s := toLanguageKey(ln)
		mouts += fmt.Sprintf("MOUT %s %v\n", s, n)
		v = "set_" + ln.Code
		incmps += fmt.Sprintf("INCMP %s %v\n", v, n)

		p := path.Join(srcDir, v)
		w, err := os.OpenFile(p, os.O_WRONLY | os.O_CREATE | os.O_EXCL, 0600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed open language set template output: %v\n", err)
			os.Exit(1)
		}
		s = toLanguageLabel(ln)
		defer w.Close()
		_, err = w.Write([]byte(s))
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed write select language vis output: %v\n", err)
			os.Exit(1)
		}
	}
	src := mouts + "HALT\n" + incmps
	src += "INCMP . *\n"

	p := path.Join(srcDir, "select_language.vis")
	w, err := os.OpenFile(p, os.O_WRONLY | os.O_CREATE | os.O_EXCL, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed open select language vis output: %v\n", err)
		os.Exit(1)
	}
	defer w.Close()
	_, err = w.Write([]byte(src))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed write select language vis output: %v\n", err)
		os.Exit(1)
	}

	src = changeHeadSrc + src
	p = path.Join(srcDir, "change_language.vis")
	w, err = os.OpenFile(p, os.O_WRONLY | os.O_CREATE | os.O_EXCL, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed open select language vis output: %v\n", err)
		os.Exit(1)
	}
	defer w.Close()
	_, err = w.Write([]byte(src))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed write select language vis output: %v\n", err)
		os.Exit(1)
	}
}
