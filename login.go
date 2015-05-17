package main

import (
	"html/template"
	"log"
	"net/http"
	"strings"
)

func forceLogin(w http.ResponseWriter, req *http.Request, ses Session) {
	if ses.Get("username") == "" {
		w.Header().Set("Location", "login")
		w.WriteHeader(http.StatusFound)
	}
}

func displayLogin(w http.ResponseWriter, req *http.Request, ses Session) {
	loginTemplate, err := template.New("base.html").ParseFiles("templates/base.html", "templates/login.html")
	if err != nil {
		log.Printf("Template error: %s", err)
		return
	}

	type loginData struct {
		Title string
		Error string
	}
	data := loginData{Error: ses.Get("loginError"), Title: "SlopeMaker Login"}
	ses.Set("loginError", "")

	err = loginTemplate.Execute(w, data)
	if err != nil {
		log.Printf("Template Error: %s", err)
	}
}

func handleLogin(w http.ResponseWriter, req *http.Request, ses Session) {
	pass, errUser := conf.String("auth", strings.ToLower(strings.TrimSpace(req.PostFormValue("username"))))
	if req.PostFormValue("password") == pass && errUser == nil {
		ses.Set("username", strings.Title(req.PostFormValue("username")))

		w.Header().Set("Location", "purger")
		w.WriteHeader(http.StatusFound)

		return
	}

	ses.Set("loginError", "Invalid username or password.")

	w.Header().Set("Location", "login")
	w.WriteHeader(http.StatusFound)
}
