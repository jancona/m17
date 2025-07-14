package main

import (
	"slices"
	"testing"
	"time"
)

func TestRepeater(t *testing.T) {
	r := NewRepeater(1)
	test := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	r.TXSamples() <- test
	time.Sleep(time.Millisecond)
	back := r.Next()
	if !slices.Equal(back[0:10], test) {
		t.Errorf("returned slice (%#v) doesn't equal sent (%#v)", back[0:10], test)
	}
}
