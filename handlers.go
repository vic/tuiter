package main

import (
	"log"
	"net/http"
)

func handleIndex(w http.ResponseWriter, r *http.Request) {
	log.Printf("DEBUG: handleIndex called - Method: %s, URL: %s", r.Method, r.URL.Path)
	session, _ := store.Get(r, sessionName)
	if session.Values["did"] == nil {
		http.Redirect(w, r, "/signin", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/timeline", http.StatusFound)
}

func handleSignin(w http.ResponseWriter, r *http.Request) {
	executeTemplate(w, "signin.html", nil)
}

func handleAbout(w http.ResponseWriter, r *http.Request) {
	log.Printf("DEBUG: handleAbout called - Method: %s, URL: %s", r.Method, r.URL.Path)
	executeTemplate(w, "about.html", nil)
}
