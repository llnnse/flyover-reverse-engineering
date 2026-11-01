package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/retroplasma/flyover-reverse-engineering/pkg/web"
)

func TestC3mmMissing(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{web.StatusError{Code: 404}, true},
		{web.StatusError{Code: 410}, true},
		{errNoData, true},
		{c3mmParseError{errors.New("bad header")}, true},
		{fmt.Errorf("wrapped: %w", web.StatusError{Code: 404}), true},
		{web.StatusError{Code: 403}, false},
		{web.StatusError{Code: 401}, false},
		{web.StatusError{Code: 503}, false},
		{errors.New("connection reset"), false},
	}
	for _, c := range cases {
		if got := c3mmMissing(c.err); got != c.want {
			t.Errorf("c3mmMissing(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
