package main

import (
	"log"
	"net/http"
	"strings"

	bsky "github.com/bluesky-social/indigo/api/bsky"
)

func handlePost(w http.ResponseWriter, r *http.Request) {
	c, didStr, err := getClientFromSession(r.Context(), r)
	if err != nil {
		http.Redirect(w, r, "/signin", http.StatusFound)
		return
	}

	data, err := preparePostPageData(r.Context(), r, c, didStr)
	if err != nil {
		http.Error(w, "Failed to prepare post page: "+err.Error(), http.StatusInternalServerError)
		return
	}

	executeTemplate(w, "post.html", data)
}

func handleProfile(w http.ResponseWriter, r *http.Request) {
	c, myDid, err := getClientFromSession(r.Context(), r)
	if err != nil {
		http.Redirect(w, r, "/signin", http.StatusFound)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/profile/")
	profileHandle := path
	if profileHandle == "" {
		profileHandle = myDid
	}

	profileView, err := fetchProfile(r.Context(), c, profileHandle)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if profileView == nil {
		http.Error(w, "Profile not found", http.StatusNotFound)
		return
	}

	authorFeed, err := bsky.FeedGetAuthorFeed(r.Context(), c, profileView.Did, "", "", false, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	myProfile, err := fetchProfile(r.Context(), c, myDid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	followsList := fetchFollows(r.Context(), c, profileView.Did, 50)

	postBoxHandle := ""
	if profileView.Handle != "" {
		postBoxHandle = profileView.Handle
	}

	// Collect parent URIs from the author's feed and batch-fetch previews
	parentURIsSet := map[string]struct{}{}
	if authorFeed != nil && authorFeed.Feed != nil {
		for _, fv := range authorFeed.Feed {
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
				log.Printf("DEBUG: handleProfile - fetchPostsBatch error: %v", err)
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
				// populate media preview if present
				if m := GetPostMedia(pv); m != nil {
					pi.Media = m
				}
				// like count if available
				if pv.LikeCount != nil {
					pi.LikeCount = int(*pv.LikeCount)
				}
				pi.IsFav = getIsFav(pv)
				parentPreviews[uri] = pi
			}
		}
	}

	data := ProfilePageData{
		Title:         "Profile - Tuiter 2006",
		Profile:       profileView,
		Feed:          authorFeed,
		Follows:       followsList,
		Posts:         PostsList{Items: authorFeed.Feed, Cursor: getCursorFromAuthorFeed(authorFeed), ParentPreviews: parentPreviews},
		PostBoxHandle: postBoxHandle,
		// provide the signed-in profile explicitly
		SignedIn: myProfile,
	}

	executeTemplate(w, "profile.html", data)
}
