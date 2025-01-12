package debug

import (
	"fmt"
	"encoding/binary"

	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	visedb "git.defalsify.org/vise.git/db"
)

var (
	dbTypStr map[storedb.DataTyp]string = make(map[storedb.DataTyp]string)
)

type KeyInfo struct {
	SessionId string
	Typ uint8
	SubTyp storedb.DataTyp
	Label string
	Description string
}

func (k KeyInfo) String() string {
	v := uint16(k.SubTyp)
	s := subTypToString(k.SubTyp)
	if s == "" {
		v = uint16(k.Typ)
		s = typToString(k.Typ)
	}
	return fmt.Sprintf("Session Id: %s\nTyp: %s (%d)\n", k.SessionId, s, v)
}

func ToKeyInfo(k []byte, sessionId string) (KeyInfo, error) {
	o := KeyInfo{}
	b := []byte(sessionId)

	if len(k) <= len(b) {
		return o, fmt.Errorf("storage key missing")
	}

	o.SessionId = sessionId

	o.Typ = uint8(k[0])
	k = k[1:]
	o.SessionId = string(k[:len(b)])
	k = k[len(b):]

	if o.Typ == visedb.DATATYPE_USERDATA {
		if len(k) == 0 {
			return o, fmt.Errorf("missing subtype key")
		}
		v := binary.BigEndian.Uint16(k[:2])
		o.SubTyp = storedb.DataTyp(v)
		o.Label = subTypToString(o.SubTyp)
		k = k[2:]
	} else {
		o.Label = typToString(o.Typ)
	}

	if len(k) != 0 {
		return o, fmt.Errorf("excess key information")
	}

	return o, nil
}

func FromKey(k []byte) (KeyInfo, error) {
	o := KeyInfo{}

	if len(k) < 4 {
		return o, fmt.Errorf("insufficient key length")
	}

	sessionIdBytes := k[1:len(k)-2]
	return ToKeyInfo(k, string(sessionIdBytes))
}

func subTypToString(v storedb.DataTyp) string {
	return dbTypStr[v + visedb.DATATYPE_USERDATA + 1]
}

func typToString(v uint8) string {
	return dbTypStr[storedb.DataTyp(uint16(v))]
}
