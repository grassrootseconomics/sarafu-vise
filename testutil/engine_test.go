package testutil

import (
	"testing"
)

func TestCreateEngine(t *testing.T) {
	o, clean, eventC := TestEngine("foo")
	defer clean()
	defer func() {
		<-eventC
		close(eventC)
	}()
	_ = o
}
