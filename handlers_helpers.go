package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/client"
)

// buildPostURIFromRequest extracts the post URI either from query param `uri` or
// by resolving a /post/{handle}/{postID} style path to an at:// URI.
func buildPostURIFromRequest(ctx context.Context, r *http.Request, c *client.APIClient) (string, error) {
	if v := r.URL.Query().Get("uri"); v != "" {
		return v, nil
	}
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/post/"), "/")
	if len(pathParts) >= 2 {
		handle := pathParts[0]
		postID := pathParts[1]
		authorDID, err := resolveHandleToDID(ctx, c, handle)
		if err != nil {
			return "", err
		}
		return "at://" + authorDID + "/app.bsky.feed.post/" + postID, nil
	}
	return "", fmt.Errorf("post uri not found")
}

// fetchThreadAndExtract fetches a post thread and returns the main post, any replies, and the thread root node.
func fetchThreadAndExtract(ctx context.Context, c *client.APIClient, postURI string) (*bsky.FeedDefs_PostView, []*bsky.FeedDefs_PostView, *bsky.FeedDefs_ThreadViewPost, error) {
	thread, err := bsky.FeedGetPostThread(ctx, c, 100, 0, postURI)
	if err != nil {
		return nil, nil, nil, err
	}
	var main *bsky.FeedDefs_PostView
	var replies []*bsky.FeedDefs_PostView
	var root *bsky.FeedDefs_ThreadViewPost
	if thread.Thread != nil && thread.Thread.FeedDefs_ThreadViewPost != nil {
		main = thread.Thread.FeedDefs_ThreadViewPost.Post
		root = thread.Thread.FeedDefs_ThreadViewPost

		// recursive collector to gather all descendant replies
		var collect func(node *bsky.FeedDefs_ThreadViewPost)
		collect = func(node *bsky.FeedDefs_ThreadViewPost) {
			if node == nil || node.Replies == nil {
				return
			}
			for _, r := range node.Replies {
				if r == nil || r.FeedDefs_ThreadViewPost == nil || r.FeedDefs_ThreadViewPost.Post == nil {
					continue
				}
				p := r.FeedDefs_ThreadViewPost.Post
				replies = append(replies, p)
				// recurse into this reply's nested replies
				collect(r.FeedDefs_ThreadViewPost)
			}
		}
		collect(thread.Thread.FeedDefs_ThreadViewPost)
	}
	return main, replies, root, nil
}

// fetchAuthorDetails resolves an author handle to DID, fetches profile and follows.
func fetchAuthorDetails(ctx context.Context, c *client.APIClient, authorHandle string) (*bsky.ActorDefs_ProfileViewDetailed, []*bsky.ActorDefs_ProfileView, error) {
	if authorHandle == "" {
		return nil, nil, fmt.Errorf("empty author handle")
	}
	did, err := resolveHandleToDID(ctx, c, authorHandle)
	if err != nil {
		return nil, nil, err
	}
	p, err := bsky.ActorGetProfile(ctx, c, did)
	if err != nil {
		return nil, nil, err
	}
	follows := fetchFollows(ctx, c, did, 50)
	return p, follows, nil
}

// extractReplyParentURI returns the parent URI if the post record contains a reply ref.
func extractReplyParentURI(pv *bsky.FeedDefs_PostView) string {
	if pv == nil || pv.Record == nil || pv.Record.Val == nil {
		return ""
	}
	if post, ok := pv.Record.Val.(*bsky.FeedPost); ok && post != nil && post.Reply != nil {
		if post.Reply.Parent != nil && post.Reply.Parent.Uri != "" {
			return post.Reply.Parent.Uri
		}
		if post.Reply.Root != nil && post.Reply.Root.Uri != "" {
			return post.Reply.Root.Uri
		}
	}
	return ""
}

// fetchPostsBatch fetches posts for the provided URIs and returns a map uri->postView
func fetchPostsBatch(ctx context.Context, c *client.APIClient, uris []string) (map[string]*bsky.FeedDefs_PostView, error) {
	if len(uris) == 0 {
		return nil, nil
	}
	resp, err := bsky.FeedGetPosts(ctx, c, uris)
	if err != nil {
		return nil, fmt.Errorf("FeedGetPosts error: %w", err)
	}
	m := make(map[string]*bsky.FeedDefs_PostView)
	for _, p := range resp.Posts {
		m[p.Uri] = p
	}
	return m, nil
}

// buildParentChain walks from an immediate parent up to the reply root (or stops at maxDepth)
// Returns chain ordered from root ... parent (chronological ancestor order)
func buildParentChain(ctx context.Context, c *client.APIClient, startURI string, maxDepth int) ([]*bsky.FeedDefs_PostView, error) {
	var chain []*bsky.FeedDefs_PostView
	currentURI := startURI
	seen := map[string]bool{}
	for depth := 0; depth < maxDepth && currentURI != ""; depth++ {
		if seen[currentURI] {
			break
		}
		seen[currentURI] = true

		// fetch current
		postsMap, err := fetchPostsBatch(ctx, c, []string{currentURI})
		if err != nil {
			return chain, err
		}
		p, ok := postsMap[currentURI]
		if !ok || p == nil {
			// not found, stop walking
			break
		}
		// prepend to chain later; collect in reverse order first
		chain = append([]*bsky.FeedDefs_PostView{p}, chain...)

		// determine next parent
		next := extractReplyParentURI(p)
		if next == "" || next == currentURI {
			break
		}
		currentURI = next
		// small sleep to avoid hammering remote servers in pathological loops
		time.Sleep(20 * time.Millisecond)
	}
	return chain, nil
}

// preparePostPageData performs the steps required to assemble PostPageData for templates.
func preparePostPageData(ctx context.Context, r *http.Request, c *client.APIClient, myDid string) (PostPageData, error) {
	postURI, err := buildPostURIFromRequest(ctx, r, c)
	if err != nil {
		return PostPageData{}, err
	}
	mainPost, replies, threadRoot, err := fetchThreadAndExtract(ctx, c, postURI)
	if err != nil {
		return PostPageData{}, err
	}
	profile, err := fetchProfile(ctx, c, myDid)
	if err != nil {
		return PostPageData{}, err
	}

	var parentChain []*bsky.FeedDefs_PostView
	// if mainPost itself is a reply, walk up to root
	if mainPost != nil {
		parentURI := extractReplyParentURI(mainPost)
		if parentURI != "" {
			chain, err := buildParentChain(ctx, c, parentURI, 20)
			if err != nil {
				// log and continue with what we have
				fmt.Printf("DEBUG: preparePostPageData - error building parent chain: %v\n", err)
			} else {
				parentChain = chain
			}
		}
	}

	var postAuthor *bsky.ActorDefs_ProfileViewDetailed
	var postAuthorFollows []*bsky.ActorDefs_ProfileView
	if mainPost != nil && mainPost.Author != nil {
		p, follows, err := fetchAuthorDetails(ctx, c, mainPost.Author.Handle)
		if err == nil {
			postAuthor = p
			postAuthorFollows = follows
		}
	}

	// Debug logging: counts and sample URIs
	if replies != nil {
		firstReply := ""
		if len(replies) > 0 && replies[0] != nil {
			firstReply = replies[0].Uri
		}
		log.Printf("DEBUG: preparePostPageData - ViewedURI=%s Replies=%d firstReply=%s ThreadRootPresent=%t ParentChainLen=%d", postURI, len(replies), firstReply, threadRoot != nil, len(parentChain))
	} else {
		log.Printf("DEBUG: preparePostPageData - ViewedURI=%s Replies=0 ThreadRootPresent=%t ParentChainLen=%d", postURI, threadRoot != nil, len(parentChain))
	}

	data := PostPageData{
		Title:             "Post - Tuiter 2006",
		Post:              mainPost,
		Replies:           replies,
		ParentChain:       parentChain,
		ViewedURI:         postURI,
		ThreadRoot:        threadRoot,
		CurrentUser:       profile,
		PostAuthor:        postAuthor,
		PostAuthorFollows: postAuthorFollows,
		// SignedIn is the profile of the currently authenticated user
		SignedIn: profile,
	}
	return data, nil
}

// prepareProfilePageData assembles ProfilePageData for rendering a user's profile page.
func prepareProfilePageData(ctx context.Context, c *client.APIClient, myDid string, profileHandle string) (ProfilePageData, error) {
	profileView, err := fetchProfile(ctx, c, profileHandle)
	if err != nil {
		return ProfilePageData{}, err
	}
	if profileView == nil {
		return ProfilePageData{}, fmt.Errorf("profile not found")
	}
	authorFeed, err := bsky.FeedGetAuthorFeed(ctx, c, profileView.Did, "", "", false, 50)
	if err != nil {
		return ProfilePageData{}, err
	}
	myProfile, err := fetchProfile(ctx, c, myDid)
	if err != nil {
		return ProfilePageData{}, err
	}
	followsList := fetchFollows(ctx, c, profileView.Did, 50)

	postBoxHandle := ""
	if profileView.Handle != "" {
		postBoxHandle = profileView.Handle
	}

	data := ProfilePageData{
		Title:         "Profile - Tuiter 2006",
		Profile:       profileView,
		Feed:          authorFeed,
		Follows:       followsList,
		Posts:         PostsList{Items: authorFeed.Feed, Cursor: getCursorFromAuthorFeed(authorFeed)},
		PostBoxHandle: postBoxHandle,
		// SignedIn is the currently authenticated profile
		SignedIn: myProfile,
	}
	return data, nil
}
