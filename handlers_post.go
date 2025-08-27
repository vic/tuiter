package main

import (
	"log"
	"net/http"

	"github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/lex/util"
)

func handlePostStatus(w http.ResponseWriter, r *http.Request) {
	c, didStr, err := getClientFromSession(r.Context(), r)
	if err != nil {
		http.Redirect(w, r, "/signin", http.StatusFound)
		return
	}

	if r.Method == http.MethodPost {
		status := r.FormValue("status")
		if status != "" {
			post := &bsky.FeedPost{Text: status}
			if _, err := atproto.RepoCreateRecord(r.Context(), c, &atproto.RepoCreateRecord_Input{
				Collection: "app.bsky.feed.post",
				Repo:       didStr,
				Record:     &util.LexiconTypeDecoder{Val: post},
			}); err != nil {
				log.Printf("DEBUG: handlePostStatus - Error creating post: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		http.Redirect(w, r, "/timeline", http.StatusFound)
		return
	}

	profile, err := fetchProfile(r.Context(), c, didStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	followsList := fetchFollows(r.Context(), c, didStr, 50)

	data := PostStatusPageData{
		Title:       "What are you doing? - Tuiter 2006",
		CurrentUser: profile,
		Profile:     profile,
		Follows:     followsList,
		SignedIn:    profile,
	}

	executeTemplate(w, "post-status.html", data)
}

func handleTimelinePost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	c, didStr, err := getClientFromSession(r.Context(), r)
	if err != nil {
		http.Error(w, "not logged in", http.StatusUnauthorized)
		return
	}

	status := r.FormValue("status")
	if status == "" {
		http.Error(w, "Status cannot be empty", http.StatusBadRequest)
		return
	}

	post := &bsky.FeedPost{Text: status}
	resp, err := atproto.RepoCreateRecord(r.Context(), c, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
		Repo:       didStr,
		Record:     &util.LexiconTypeDecoder{Val: post},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Println("Created post:", resp.Uri)

	w.Header().Set("Content-Type", "text/html")
	timeline, err := bsky.FeedGetTimeline(r.Context(), c, "", "", 50)
	if err != nil {
		http.Error(w, "Failed to load timeline", http.StatusInternalServerError)
		return
	}

	// fetch signed-in profile for template context
	signedInProfile, _ := fetchProfile(r.Context(), c, didStr)

	data := TimelinePartialData{Timeline: timeline, Posts: PostsList{Items: timeline.Feed, Cursor: getCursorFromTimeline(timeline)}, SignedIn: signedInProfile}
	if err := tpl.ExecuteTemplate(w, "timeline_posts_partial.html", data); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
