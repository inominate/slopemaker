package main

import (
	"github.com/codegangsta/martini"
	"net/http"
)

type Session interface {
	Get(key string) string
	Set(key string, value string)
	Clear()

	NewActionToken() string
	CanAct() bool
	ActionToken() string
}

func SessionService() martini.Handler {
	return func(w http.ResponseWriter, req *http.Request, c martini.Context) {
		s, _ := sesManager.Begin(w, req)
		c.MapTo(s, (*Session)(nil))

		c.Next()
		s.Commit()
	}
}
