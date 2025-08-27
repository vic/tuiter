package main

import (
	"log"
	"net/http"

	"github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/lex/util"
)

func handleTimeline(w http.ResponseWriter, r *http.Request) {
	log.Printf("DEBUG: handleTimeline called - Method: %s, URL: %s", r.Method, r.URL.Path)
	c, didStr, err := getClientFromSession(r.Context(), r)
	if err != nil {
		http.Redirect(w, r, "/signin", http.StatusFound)
		return
	}

	profile, err := fetchProfile(r.Context(), c, didStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	timeline, err := bsky.FeedGetTimeline(r.Context(), c, "", "", 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	followsList := fetchFollows(r.Context(), c, didStr, 50)

	// Collect all unique parent URIs referenced in the timeline so we can batch-fetch them once
	parentURIsSet := map[string]struct{}{}
	if timeline != nil && timeline.Feed != nil {
		for _, fv := range timeline.Feed {
			if fv == nil || fv.Post == nil {
				continue
			}
			if uri := extractReplyParentURI(fv.Post); uri != "" {
				parentURIsSet[uri] = struct{}{}
			}
			// also include any root refs from the post record if present
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

	// Batch fetch parent posts (API limits 25 URIs per request)
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
				log.Printf("DEBUG: handleTimeline - fetchPostsBatch error: %v", err)
				continue
			}
			for uri, pv := range postsMap {
				if pv == nil {
					continue
				}
				pi := ParentInfo{Uri: uri}
				// fill author handle/name if present
				if pv.Author != nil {
					if pv.Author.DisplayName != nil && *pv.Author.DisplayName != "" {
						pi.AuthorName = *pv.Author.DisplayName
					} else if pv.Author.Handle != "" {
						pi.AuthorName = pv.Author.Handle
					}
					if pv.Author.Handle != "" {
						pi.AuthorHandle = pv.Author.Handle
					}
					// populate avatar if available
					if pv.Author.Avatar != nil {
						pi.Avatar = *pv.Author.Avatar
					}
				}
				// fill text
				pi.Text = getPostText(pv.Record)
				// link URL and posted time if available
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
				// viewer state: whether the signed-in viewer liked this post
				pi.IsFav = getIsFav(pv)
				parentPreviews[uri] = pi
			}
		}
	}

	postsList := PostsList{Items: timeline.Feed, Cursor: getCursorFromTimeline(timeline), ParentPreviews: parentPreviews}

	data := TimelinePageData{
		Title:         "Timeline - Tuiter 2006",
		CurrentUser:   profile,
		Profile:       profile,
		Timeline:      timeline,
		Follows:       followsList,
		Posts:         postsList,
		PostBoxHandle: "",
		// SignedIn should point to the logged-in profile
		SignedIn: profile,
	}

	executeTemplate(w, "timeline.html", data)
}

func handleReply(w http.ResponseWriter, r *http.Request) {
	log.Printf("DEBUG: handleReply called - Method: %s, URL: %s", r.Method, r.URL.Path)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	c, didStr, err := getClientFromSession(r.Context(), r)
	if err != nil {
		http.Redirect(w, r, "/signin", http.StatusFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form data", http.StatusInternalServerError)
		return
	}

	replyTo := r.FormValue("reply-to")
	status := r.FormValue("status")
	if replyTo == "" || status == "" {
		http.Error(w, "reply-to and status are required", http.StatusBadRequest)
		return
	}

	postsView, err := bsky.FeedGetPosts(r.Context(), c, []string{replyTo})
	if err != nil || len(postsView.Posts) == 0 {
		log.Printf("DEBUG: handleReply - Error fetching original post: %v", err)
		http.Error(w, "Original post not found", http.StatusNotFound)
		return
	}

	postView := postsView.Posts[0]
	post := &bsky.FeedPost{
		Text:      status,
		CreatedAt: "",
		Reply: &bsky.FeedPost_ReplyRef{
			Root:   &atproto.RepoStrongRef{Uri: postView.Uri, Cid: postView.Cid},
			Parent: &atproto.RepoStrongRef{Uri: postView.Uri, Cid: postView.Cid},
		},
	}

	resp, err := atproto.RepoCreateRecord(r.Context(), c, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
		Repo:       didStr,
		Record:     &util.LexiconTypeDecoder{Val: post},
	})
	if err != nil {
		http.Error(w, "Failed to create reply: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Println("Created reply:", resp.Uri)
	http.Redirect(w, r, "/post?uri="+replyTo, http.StatusFound)
}
