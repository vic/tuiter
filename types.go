package main

import (
	bsky "github.com/bluesky-social/indigo/api/bsky"
)

// Page data structs

type PostStatusPageData struct {
	Title       string
	CurrentUser *bsky.ActorDefs_ProfileViewDetailed
	Profile     *bsky.ActorDefs_ProfileViewDetailed
	Follows     []*bsky.ActorDefs_ProfileView
	// SignedIn is the currently signed-in profile (typed, may be nil)
	SignedIn *bsky.ActorDefs_ProfileViewDetailed
}

type TimelinePageData struct {
	Title         string
	CurrentUser   *bsky.ActorDefs_ProfileViewDetailed
	Profile       *bsky.ActorDefs_ProfileViewDetailed
	Timeline      *bsky.FeedGetTimeline_Output
	Follows       []*bsky.ActorDefs_ProfileView
	Posts         PostsList
	PostBoxHandle string
	// SignedIn is the currently signed-in profile (typed, may be nil)
	SignedIn *bsky.ActorDefs_ProfileViewDetailed
}

type TimelinePartialData struct {
	Timeline *bsky.FeedGetTimeline_Output
	Posts    PostsList
	// SignedIn may be present for partials that need header links
	SignedIn *bsky.ActorDefs_ProfileViewDetailed
}

type PostPageData struct {
	Title             string
	Post              *bsky.FeedDefs_PostView
	Replies           []*bsky.FeedDefs_PostView
	ParentChain       []*bsky.FeedDefs_PostView
	ViewedURI         string
	ThreadRoot        *bsky.FeedDefs_ThreadViewPost
	CurrentUser       *bsky.ActorDefs_ProfileViewDetailed
	PostAuthor        *bsky.ActorDefs_ProfileViewDetailed
	PostAuthorFollows []*bsky.ActorDefs_ProfileView
	// SignedIn is the currently signed-in profile (typed, may be nil)
	SignedIn *bsky.ActorDefs_ProfileViewDetailed
}

type ProfilePageData struct {
	Title         string
	Profile       *bsky.ActorDefs_ProfileViewDetailed
	Feed          *bsky.FeedGetAuthorFeed_Output
	Follows       []*bsky.ActorDefs_ProfileView
	Posts         PostsList
	PostBoxHandle string
	// SignedIn is the currently signed-in profile (typed, may be nil)
	SignedIn *bsky.ActorDefs_ProfileViewDetailed
}

type TimelineProvider struct{ T *bsky.FeedGetTimeline_Output }

func (p TimelineProvider) Posts() []*bsky.FeedDefs_FeedViewPost {
	if p.T == nil || p.T.Feed == nil {
		return nil
	}
	return p.T.Feed
}

func (p TimelineProvider) Cursor() string {
	if p.T == nil || p.T.Cursor == nil {
		return ""
	}
	return *p.T.Cursor
}

type AuthorProvider struct {
	F *bsky.FeedGetAuthorFeed_Output
}

func (p AuthorProvider) Posts() []*bsky.FeedDefs_FeedViewPost {
	if p.F == nil || p.F.Feed == nil {
		return nil
	}
	return p.F.Feed
}

func (p AuthorProvider) Cursor() string { return "" }
