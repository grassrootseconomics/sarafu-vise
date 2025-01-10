package args

import (
	"strings"

	"git.defalsify.org/vise.git/lang"
)

type LangVar struct {
	v []lang.Language
}

func(lv *LangVar) Set(s string) error {
	v, err := lang.LanguageFromCode(s)
	if err != nil {
		return err
	}
	lv.v = append(lv.v, v)
	return err
}

func(lv *LangVar) String() string {
	var s []string
	for _, v := range(lv.v) {
		s = append(s, v.Code)
	}
	return strings.Join(s, ",")
}

func(lv *LangVar) Langs() []lang.Language {
	return lv.v
}


