package main

import (
	"fmt"
	"log"
	"net/http"

	bsky "github.com/bluesky-social/indigo/api/bsky"
)

func htmxTimelineFeed(w http.ResponseWriter, r *http.Request) {
	c, _, err := getClientFromSession(r.Context(), r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	cursor := r.URL.Query().Get("cursor")
	timeline, err := bsky.FeedGetTimeline(r.Context(), c, "", cursor, 50)
	if err != nil {
		log.Printf("DEBUG: htmxTimelineFeed - Error fetching timeline: %v", err)
		http.Error(w, "Failed to load timeline", http.StatusInternalServerError)
		return
	}

	// Collect parent URIs and batch-fetch previews
	parentURIsSet := map[string]struct{}{}
	if timeline != nil && timeline.Feed != nil {
		for _, fv := range timeline.Feed {
			if fv == nil || fv.Post == nil {
				continue
			}
			if uri := extractReplyParentURI(fv.Post); uri != "" {
				parentURIsSet[uri] = struct{}{}
			}
			chain := GetReplyChainInfos(fv.Post)
			for _, pi := range chain {
				if pi.Uri != "" {
					parentURIsSet[pi.Uri] = struct{}{}
				}
			}
		}
	}

	var parentURIs []string
	for u := range parentURIsSet {
		parentURIs = append(parentURIs, u)
	}

	parentPreviews := map[string]ParentInfo{}
	if len(parentURIs) > 0 {
		const batchSize = 25
		for i := 0; i < len(parentURIs); i += batchSize {
			end := i + batchSize
			if end > len(parentURIs) {
				end = len(parentURIs)
			}
			batch := parentURIs[i:end]
			postsMap, err := fetchPostsBatch(r.Context(), c, batch)
			if err != nil {
				log.Printf("DEBUG: htmxTimelineFeed - fetchPostsBatch error: %v", err)
				continue
			}
			for uri, pv := range postsMap {
				if pv == nil {
					continue
				}
				pi := ParentInfo{Uri: uri}
				if pv.Author != nil {
					if pv.Author.DisplayName != nil && *pv.Author.DisplayName != "" {
						pi.AuthorName = *pv.Author.DisplayName
					} else if pv.Author.Handle != "" {
						pi.AuthorName = pv.Author.Handle
					}
					if pv.Author.Handle != "" {
						pi.AuthorHandle = pv.Author.Handle
					}
					if pv.Author.Avatar != nil {
						pi.Avatar = *pv.Author.Avatar
					}
				}
				pi.Text = getPostText(pv.Record)
				if pv.Uri != "" {
					pi.PostURL = getPostURL(pv)
				}
				pi.IndexedAt = pv.IndexedAt
				if m := GetPostMedia(pv); m != nil {
					pi.Media = m
				}
				// like count if available
				if pv.LikeCount != nil {
					pi.LikeCount = int(*pv.LikeCount)
				}
				pi.ReplyCount = int(*pv.ReplyCount)
				pi.RepostCount = int(*pv.RepostCount)
				pi.IsFav = getIsFav(pv)
				parentPreviews[uri] = pi
			}
		}
	}

	w.Header().Set("Content-Type", "text/html")
	data := TimelinePartialData{Timeline: timeline, Posts: PostsList{Items: timeline.Feed, Cursor: getCursorFromTimeline(timeline), ParentPreviews: parentPreviews}}
	if err := tpl.ExecuteTemplate(w, "timeline_posts_partial.html", data); err != nil {
		log.Printf("DEBUG: htmxTimelineFeed - Template error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	nextCursor := getCursorFromTimeline(timeline)
	tmplData := struct{ Cursor string }{Cursor: nextCursor}
	if err := tpl.ExecuteTemplate(w, "timeline_more.html", tmplData); err != nil {
		log.Printf("DEBUG: htmxTimelineFeed - failed to execute timeline_more template: %v", err)
		fmt.Fprint(w, `<div id="timeline-more" hx-swap-oob="innerHTML"></div>`)
	}
}

func htmxProfileFeed(w http.ResponseWriter, r *http.Request) {
	c, _, err := getClientFromSession(r.Context(), r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	did := r.URL.Query().Get("did")
	cursor := r.URL.Query().Get("cursor")

	feed, err := bsky.FeedGetAuthorFeed(r.Context(), c, did, "", cursor, false, 50)
	if err != nil {
		log.Printf("DEBUG: htmxProfileFeed - Error fetching author feed for %s: %v", did, err)
		http.Error(w, "Failed to load profile posts", http.StatusInternalServerError)
		return
	}

	// Collect parent URIs for this author feed and batch-fetch previews
	parentURIsSet := map[string]struct{}{}
	if feed != nil && feed.Feed != nil {
		for _, fv := range feed.Feed {
			if fv == nil || fv.Post == nil {
				continue
			}
			if uri := extractReplyParentURI(fv.Post); uri != "" {
				parentURIsSet[uri] = struct{}{}
			}
			chain := GetReplyChainInfos(fv.Post)
			for _, pi := range chain {
				if pi.Uri != "" {
					parentURIsSet[pi.Uri] = struct{}{}
				}
			}
		}
	}

	var parentURIs []string
	for u := range parentURIsSet {
		parentURIs = append(parentURIs, u)
	}

	parentPreviews := map[string]ParentInfo{}
	if len(parentURIs) > 0 {
		const batchSize = 25
		for i := 0; i < len(parentURIs); i += batchSize {
			end := i + batchSize
			if end > len(parentURIs) {
				end = len(parentURIs)
			}
			batch := parentURIs[i:end]
			postsMap, err := fetchPostsBatch(r.Context(), c, batch)
			if err != nil {
				log.Printf("DEBUG: htmxProfileFeed - fetchPostsBatch error: %v", err)
				continue
			}
			for uri, pv := range postsMap {
				if pv == nil {
					continue
				}
				pi := ParentInfo{Uri: uri}
				if pv.Author != nil {
					if pv.Author.DisplayName != nil && *pv.Author.DisplayName != "" {
						pi.AuthorName = *pv.Author.DisplayName
					} else if pv.Author.Handle != "" {
						pi.AuthorName = pv.Author.Handle
					}
					if pv.Author.Handle != "" {
						pi.AuthorHandle = pv.Author.Handle
					}
					if pv.Author.Avatar != nil {
						pi.Avatar = *pv.Author.Avatar
					}
				}
				pi.Text = getPostText(pv.Record)
				if pv.Uri != "" {
					pi.PostURL = getPostURL(pv)
				}
				pi.IndexedAt = pv.IndexedAt
				if m := GetPostMedia(pv); m != nil {
					pi.Media = m
				}
				// like count if available
				if pv.LikeCount != nil {
					pi.LikeCount = int(*pv.LikeCount)
				}
				pi.ReplyCount = int(*pv.ReplyCount)
				pi.RepostCount = int(*pv.RepostCount)
				pi.IsFav = getIsFav(pv)
				parentPreviews[uri] = pi
			}
		}
	}

	w.Header().Set("Content-Type", "text/html")
	postsData := PostsList{Items: feed.Feed, Cursor: getCursorFromAuthorFeed(feed), ParentPreviews: parentPreviews}
	if err := tpl.ExecuteTemplate(w, "posts_list_partial.html", postsData); err != nil {
		log.Printf("DEBUG: htmxProfileFeed - Template error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	tmplData := struct{ Did, Cursor string }{Did: did, Cursor: postsData.Cursor}
	if err := tpl.ExecuteTemplate(w, "profile_more.html", tmplData); err != nil {
		log.Printf("DEBUG: htmxProfileFeed - failed to execute profile_more template: %v", err)
		fmt.Fprint(w, `<div id="profile-more" hx-swap-oob="innerHTML"></div>`)
	}
}
