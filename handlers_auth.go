package main

import (
	"encoding/json"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

func handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identifier := r.FormValue("identifier")
	if identifier == "" {
		http.Error(w, "identifier is required", http.StatusBadRequest)
		return
	}
	redirectURL, err := oauthApp.StartAuthFlow(ctx, identifier)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, sessionName)

	didStr, ok := session.Values["did"].(string)
	if ok {
		did, err := syntax.ParseDID(didStr)
		if err == nil {
			sessionID, ok := session.Values["session_id"].(string)
			if ok {
				oauthApp.Store.DeleteSession(r.Context(), did, sessionID)
			}
		}
	}

	session.Values["did"] = nil
	session.Values["session_id"] = nil
	session.Save(r, w)
	http.Redirect(w, r, "/signin", http.StatusFound)
}

func handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sessData, err := oauthApp.ProcessCallback(ctx, r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, sessionName)
	session.Values["did"] = sessData.AccountDID.String()
	session.Values["session_id"] = sessData.SessionID
	err = session.Save(r, w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/timeline", http.StatusFound)
}

func handleClientMetadata(w http.ResponseWriter, r *http.Request) {
	doc := oauthApp.Config.ClientMetadata()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(doc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
