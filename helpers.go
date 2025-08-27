package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/client"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/lex/util"
)

// PostsList is a small, type-safe wrapper passed to templates to render lists of posts
// and provide optional pagination cursor in a single place.
type PostsList struct {
	Items  []*bsky.FeedDefs_FeedViewPost
	Cursor string
	// ParentPreviews holds pre-fetched ParentInfo keyed by parent URI. Handlers should populate
	// this map by collecting all reply-ref URIs and calling fetchPostsBatch once.
	ParentPreviews map[string]ParentInfo
}

func getPostText(record *util.LexiconTypeDecoder) string {
	if record == nil || record.Val == nil {
		log.Printf("DEBUG: getPostText - record or record.Val is nil")
		return "[Post content unavailable]"
	}
	if post, ok := record.Val.(*bsky.FeedPost); ok && post != nil {
		return post.Text
	}
	log.Printf("DEBUG: getPostText - unable to extract text from record type: %T", record.Val)
	return "[Post content unavailable]"
}

func resolveHandleToDID(ctx context.Context, c *client.APIClient, identifier string) (string, error) {
	if strings.HasPrefix(identifier, "did:") {
		return identifier, nil
	}
	profile, err := bsky.ActorGetProfile(ctx, c, identifier)
	if err != nil {
		log.Printf("DEBUG: resolveHandleToDID - error resolving handle %s: %v", identifier, err)
		return "", err
	}
	return profile.Did, nil
}

func executeTemplate(w http.ResponseWriter, templateName string, data interface{}) {
	// Ensure templates that reference .SignedIn won't panic when handlers pass nil
	if data == nil {
		// minimal typed wrapper with SignedIn nil
		data = struct {
			SignedIn *bsky.ActorDefs_ProfileViewDetailed
		}{}
	}
	if err := tpl.ExecuteTemplate(w, templateName, data); err != nil {
		log.Printf("Template execution error for %s: %v", templateName, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func getIntFromProfile(obj interface{}, keys []string) int {
	if obj == nil {
		return 0
	}
	switch p := obj.(type) {
	case *bsky.ActorDefs_ProfileViewDetailed:
		if p == nil {
			return 0
		}
		for _, k := range keys {
			switch k {
			case "followersCount", "followers_count", "followers":
				if p.FollowersCount != nil {
					return int(*p.FollowersCount)
				}
			case "followsCount", "follows_count", "following", "follows":
				if p.FollowsCount != nil {
					return int(*p.FollowsCount)
				}
			case "postsCount", "posts_count", "posts":
				if p.PostsCount != nil {
					return int(*p.PostsCount)
				}
			}
		}
	default:
		return 0
	}
	return 0
}

func getDisplayNameFromProfile(obj interface{}) string {
	if obj == nil {
		return ""
	}
	switch p := obj.(type) {
	case *bsky.ActorDefs_ProfileViewDetailed:
		if p == nil {
			return ""
		}
		if p.DisplayName != nil && *p.DisplayName != "" {
			return *p.DisplayName
		}
		if p.Handle != "" {
			return p.Handle
		}
		return ""
	case *bsky.ActorDefs_ProfileView:
		if p == nil {
			return ""
		}
		if p.DisplayName != nil && *p.DisplayName != "" {
			return *p.DisplayName
		}
		if p.Handle != "" {
			return p.Handle
		}
		return ""
	case *bsky.ActorDefs_ProfileViewBasic:
		if p == nil {
			return ""
		}
		if p.DisplayName != nil && *p.DisplayName != "" {
			return *p.DisplayName
		}
		if p.Handle != "" {
			return p.Handle
		}
		return ""
	default:
		return ""
	}
}

func getClientFromSession(ctx context.Context, r *http.Request) (*client.APIClient, string, error) {
	session, _ := store.Get(r, sessionName)
	didStr, ok := session.Values["did"].(string)
	if !ok || didStr == "" {
		return nil, "", fmt.Errorf("not logged in")
	}
	sessionID, ok := session.Values["session_id"].(string)
	if !ok || sessionID == "" {
		return nil, "", fmt.Errorf("not logged in")
	}
	did, err := syntax.ParseDID(didStr)
	if err != nil {
		return nil, "", err
	}
	sess, err := oauthApp.ResumeSession(ctx, did, sessionID)
	if err != nil {
		return nil, "", err
	}
	return sess.APIClient(), didStr, nil
}

func fetchFollows(ctx context.Context, c *client.APIClient, did string, limit int64) []*bsky.ActorDefs_ProfileView {
	follows, err := bsky.GraphGetFollows(ctx, c, did, "", limit)
	if err != nil {
		log.Printf("DEBUG: fetchFollows - error fetching follows for %s: %v", did, err)
		return nil
	}
	if follows == nil || follows.Follows == nil {
		return nil
	}
	return follows.Follows
}

func fetchProfile(ctx context.Context, c *client.APIClient, idOrHandle string) (*bsky.ActorDefs_ProfileViewDetailed, error) {
	if idOrHandle == "" {
		return nil, fmt.Errorf("empty identifier")
	}
	if !strings.HasPrefix(idOrHandle, "did:") {
		resolved, err := resolveHandleToDID(ctx, c, idOrHandle)
		if err != nil {
			return nil, err
		}
		idOrHandle = resolved
	}
	profile, err := bsky.ActorGetProfile(ctx, c, idOrHandle)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func getCursorFromTimeline(t *bsky.FeedGetTimeline_Output) string {
	if t == nil {
		return ""
	}
	if t.Cursor != nil {
		return *t.Cursor
	}
	return ""
}

func getCursorFromAuthorFeed(f *bsky.FeedGetAuthorFeed_Output) string {
	if f == nil {
		return ""
	}
	if f.Cursor != nil {
		return *f.Cursor
	}
	return ""
}

func getCursorFromAny(v interface{}) string {
	switch t := v.(type) {
	case *bsky.FeedGetTimeline_Output:
		return getCursorFromTimeline(t)
	case *bsky.FeedGetAuthorFeed_Output:
		return getCursorFromAuthorFeed(t)
	default:
		return ""
	}
}

func getProfileURL(actor interface{}) string {
	switch a := actor.(type) {
	case *bsky.ActorDefs_ProfileView:
		if a != nil && a.Handle != "" {
			return "/profile/" + a.Handle
		}
	case *bsky.ActorDefs_ProfileViewBasic:
		if a != nil && a.Handle != "" {
			return "/profile/" + a.Handle
		}
	case *bsky.ActorDefs_ProfileViewDetailed:
		if a != nil && a.Handle != "" {
			return "/profile/" + a.Handle
		}
	}
	return "#"
}

func getPostURL(post *bsky.FeedDefs_PostView) string {
	if post != nil && post.Author != nil && post.Author.Handle != "" && post.Uri != "" {
		uriParts := strings.Split(post.Uri, "/")
		if len(uriParts) >= 4 {
			postID := uriParts[len(uriParts)-1]
			return "/post/" + post.Author.Handle + "/" + postID
		}
	}
	return "#"
}

func getFollowingCount(actor interface{}) int {
	return getIntFromProfile(actor, []string{"followsCount", "follows_count", "following", "follows"})
}
func getFollowersCount(actor interface{}) int {
	return getIntFromProfile(actor, []string{"followersCount", "followers_count", "followers"})
}
func getPostsCount(actor interface{}) int {
	return getIntFromProfile(actor, []string{"postsCount", "posts_count", "posts"})
}

// Post type helpers

type PostType int

const (
	PostTypeAuthored PostType = iota
	PostTypeRetweet
	PostTypeQuote
)

func GetPostType(fvp *bsky.FeedDefs_FeedViewPost) PostType {
	if fvp == nil || fvp.Post == nil {
		return PostTypeAuthored
	}
	if fvp.Reason != nil && fvp.Reason.FeedDefs_ReasonRepost != nil {
		return PostTypeRetweet
	}
	if fvp.Post.Embed != nil && fvp.Post.Embed.EmbedRecord_View != nil {
		return PostTypeQuote
	}
	return PostTypeAuthored
}

func GetPostPrefix(fvp *bsky.FeedDefs_FeedViewPost) string {
	switch GetPostType(fvp) {
	case PostTypeRetweet:
		return "RT"
	case PostTypeQuote:
		return "QT"
	default:
		return ""
	}
}

// Convenience boolean helpers for templates
func IsPostRetweet(item interface{}) bool {
	switch it := item.(type) {
	case *bsky.FeedDefs_FeedViewPost:
		return GetPostType(it) == PostTypeRetweet
	default:
		return false
	}
}

func IsPostQuote(item interface{}) bool {
	switch it := item.(type) {
	case *bsky.FeedDefs_FeedViewPost:
		return GetPostType(it) == PostTypeQuote
	default:
		return false
	}
}

// Embed helpers

type EmbedRecordViewRecord struct {
	Author interface{}
	Value  *util.LexiconTypeDecoder
}

func GetEmbedRecord(post *bsky.FeedDefs_PostView) *EmbedRecordViewRecord {
	if post == nil || post.Embed == nil || post.Embed.EmbedRecord_View == nil || post.Embed.EmbedRecord_View.Record == nil {
		return nil
	}
	recordWrapper := post.Embed.EmbedRecord_View.Record
	if recordWrapper.EmbedRecord_ViewRecord == nil {
		return nil
	}
	rr := recordWrapper.EmbedRecord_ViewRecord
	return &EmbedRecordViewRecord{Author: rr.Author, Value: rr.Value}
}

type EmbedTemplateContext struct {
	Parent *bsky.FeedDefs_PostView
	Embed  *EmbedRecordViewRecord
}

func embedContext(parent *bsky.FeedDefs_PostView, embed *EmbedRecordViewRecord) *EmbedTemplateContext {
	return &EmbedTemplateContext{Parent: parent, Embed: embed}
}

// Small avatar/banner helpers to keep templates simple and avoid repeating conditionals.
func AvatarURL(actor interface{}) string {
	switch a := actor.(type) {
	case *bsky.ActorDefs_ProfileView:
		if a != nil && a.Avatar != nil {
			return *a.Avatar
		}
	case *bsky.ActorDefs_ProfileViewBasic:
		if a != nil && a.Avatar != nil {
			return *a.Avatar
		}
	case *bsky.ActorDefs_ProfileViewDetailed:
		if a != nil && a.Avatar != nil {
			return *a.Avatar
		}
	case *bsky.FeedDefs_PostView:
		// allow passing a PostView directly
		if a != nil && a.Author != nil && a.Author.Avatar != nil {
			return *a.Author.Avatar
		}
	}
	return ""
}

func HasAvatar(actor interface{}) bool {
	return AvatarURL(actor) != ""
}

func BannerURL(actor interface{}) string {
	switch a := actor.(type) {
	case *bsky.ActorDefs_ProfileViewDetailed:
		if a != nil && a.Banner != nil {
			return *a.Banner
		}
	}
	return ""
}

// Post box helpers
func PostBoxInitial(handle string) string {
	if handle == "" {
		return ""
	}
	return "@" + handle + " "
}

func PostBoxPlaceholder(handle string) string {
	if handle == "" {
		return "What are you doing?"
	}
	return "Mention " + handle
}

// PostVM is a small, template-friendly view model for posts.
type PostVM struct {
	AuthorDisplayName string
	AuthorHandle      string
	AuthorAvatar      string
	Text              string
	PostURL           string
	IndexedAt         string
	ReplyCount        int
	IsQuote           bool
	IsRetweet         bool
	ParentPost        *bsky.FeedDefs_PostView
	EmbedRecord       *EmbedRecordViewRecord
	Raw               *bsky.FeedDefs_FeedViewPost // keep raw for advanced helpers if needed
}

// BuildPostVM converts a typed feed view post into a PostVM for templates.
func BuildPostVM(ctx context.Context, item *bsky.FeedDefs_FeedViewPost) *PostVM {
	if item == nil || item.Post == nil {
		return nil
	}
	post := item.Post
	vm := &PostVM{Raw: item}
	// author
	if post.Author != nil {
		if post.Author.Handle != "" {
			vm.AuthorHandle = post.Author.Handle
		}
		if post.Author.DisplayName != nil && *post.Author.DisplayName != "" {
			vm.AuthorDisplayName = *post.Author.DisplayName
		} else if post.Author.Handle != "" {
			vm.AuthorDisplayName = post.Author.Handle
		}
		if post.Author.Avatar != nil && *post.Author.Avatar != "" {
			vm.AuthorAvatar = *post.Author.Avatar
		}
	}
	// text and metadata
	vm.Text = getPostText(post.Record)
	vm.PostURL = getPostURL(post)
	vm.IndexedAt = post.IndexedAt
	if post.ReplyCount != nil {
		vm.ReplyCount = int(*post.ReplyCount)
	}
	// embed / type
	vm.IsRetweet = item.Reason != nil && item.Reason.FeedDefs_ReasonRepost != nil
	vm.IsQuote = post.Embed != nil && post.Embed.EmbedRecord_View != nil
	// parent and embed record
	if post != nil {
		vm.ParentPost = post
	}
	if vm.IsQuote {
		vm.EmbedRecord = GetEmbedRecord(post)
	}
	return vm
}

// Helper wrapper exposed to templates: convert interface{} (feed item) to *PostVM
func buildPostVMForTemplate(item interface{}) *PostVM {
	switch it := item.(type) {
	case *bsky.FeedDefs_FeedViewPost:
		return BuildPostVM(context.Background(), it)
	default:
		log.Printf("DEBUG: buildPostVMForTemplate - unexpected type %T", item)
		return nil
	}
}

// Add helper to expose LikeCount safely to templates.
func getLikeCount(post *bsky.FeedDefs_PostView) int {
	if post == nil || post.LikeCount == nil {
		return 0
	}
	return int(*post.LikeCount)
}

// MakeElementID converts an at:// URI into a safe DOM id (alphanumeric and dashes)
func MakeElementID(uri string) string {
	if uri == "" {
		return ""
	}
	// remove scheme prefix if present
	uri = strings.TrimPrefix(uri, "at://")
	// replace non-alphanumeric characters with dash
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	id := re.ReplaceAllString(uri, "-")
	// ensure doesn't start with digit-only? keep as-is
	return "post-" + strings.Trim(id, "-")
}

// ThreadNodeWrapper bundles a ThreadViewPost with the ViewedURI so templates can access both typed values safely.
type ThreadNodeWrapper struct {
	Post      *bsky.FeedDefs_ThreadViewPost
	ViewedURI string
}

// wrapThread is a template helper that wraps a ThreadViewPost with the current viewed URI.
func wrapThread(n *bsky.FeedDefs_ThreadViewPost, viewedURI string) ThreadNodeWrapper {
	return ThreadNodeWrapper{Post: n, ViewedURI: viewedURI}
}

// HasItems is a tiny helper to ask if a PostsList has items; keeps templates readable.
func HasItems(pl *PostsList) bool {
	return pl != nil && len(pl.Items) > 0
}

// Media view models for templates
type ImageVM struct {
	Thumb string
	Full  string
	Alt   string
}

type VideoVM struct {
	Thumb    string
	Cid      string
	Playlist string
	OwnerDid string
}

type ExternalVM struct {
	Title       string
	Description string
	Thumb       string
	Uri         string
}

type MediaVM struct {
	Images   []ImageVM
	Video    *VideoVM
	External *ExternalVM
}

// GetPostMedia inspects a post's embed fields and returns a small, typed
// MediaVM suitable for templates. It supports images, videos (thumbnail only)
// and external link previews. Returns nil if no media present.
func GetPostMedia(post *bsky.FeedDefs_PostView) *MediaVM {
	if post == nil || post.Embed == nil {
		return nil
	}
	m := &MediaVM{}

	// top-level images
	if post.Embed.EmbedImages_View != nil && post.Embed.EmbedImages_View.Images != nil {
		for _, im := range post.Embed.EmbedImages_View.Images {
			if im == nil {
				continue
			}
			m.Images = append(m.Images, ImageVM{Thumb: im.Thumb, Full: im.Fullsize, Alt: im.Alt})
		}
	}

	// recordWithMedia (nested media inside an embedded record)
	if post.Embed.EmbedRecordWithMedia_View != nil && post.Embed.EmbedRecordWithMedia_View.Media != nil {
		mm := post.Embed.EmbedRecordWithMedia_View.Media
		if mm.EmbedImages_View != nil && mm.EmbedImages_View.Images != nil {
			for _, im := range mm.EmbedImages_View.Images {
				if im == nil {
					continue
				}
				m.Images = append(m.Images, ImageVM{Thumb: im.Thumb, Full: im.Fullsize, Alt: im.Alt})
			}
		}
		if mm.EmbedVideo_View != nil {
			v := mm.EmbedVideo_View
			var thumb string
			if v.Thumbnail != nil {
				thumb = *v.Thumbnail
			}
			ownerDid := ""
			if post.Author != nil {
				ownerDid = post.Author.Did
			}
			m.Video = &VideoVM{Thumb: thumb, Cid: v.Cid, Playlist: v.Playlist, OwnerDid: ownerDid}
		}
		if mm.EmbedExternal_View != nil && mm.EmbedExternal_View.External != nil {
			ex := mm.EmbedExternal_View.External
			extVM := &ExternalVM{Title: ex.Title, Description: ex.Description, Uri: ex.Uri}
			if ex.Thumb != nil {
				extVM.Thumb = *ex.Thumb
			}
			m.External = extVM
		}
	}

	// top-level external
	if post.Embed.EmbedExternal_View != nil && post.Embed.EmbedExternal_View.External != nil {
		ex := post.Embed.EmbedExternal_View.External
		extVM := &ExternalVM{Title: ex.Title, Description: ex.Description, Uri: ex.Uri}
		if ex.Thumb != nil {
			extVM.Thumb = *ex.Thumb
		}
		m.External = extVM
	}

	// top-level video view
	if post.Embed.EmbedVideo_View != nil {
		v := post.Embed.EmbedVideo_View
		var thumb string
		if v.Thumbnail != nil {
			thumb = *v.Thumbnail
		}
		ownerDid := ""
		if post.Author != nil {
			ownerDid = post.Author.Did
		}
		m.Video = &VideoVM{Thumb: thumb, Cid: v.Cid, Playlist: v.Playlist, OwnerDid: ownerDid}
	}

	if len(m.Images) == 0 && m.Video == nil && m.External == nil {
		return nil
	}
	return m
}

// GetMediaForTemplate accepts either a *bsky.FeedDefs_PostView or a *MediaVM and returns a *MediaVM
// This lets templates call a single helper when they may have either the full PostView or a precomputed MediaVM.
func GetMediaForTemplate(v interface{}) *MediaVM {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case *bsky.FeedDefs_PostView:
		return GetPostMedia(t)
	case *MediaVM:
		return t
	default:
		return nil
	}
}

// IsPostReply reports whether the given feed item is a reply (has a Reply ref).
func IsPostReply(item interface{}) bool {
	switch it := item.(type) {
	case *bsky.FeedDefs_FeedViewPost:
		if it == nil || it.Post == nil {
			return false
		}
		// consider it a reply only if a parent/root URI is present
		parentURI := extractReplyParentURI(it.Post)
		return parentURI != ""
	default:
		return false
	}
}

// ReplyParentURI returns the parent URI for a post's reply reference, or empty string.
func ReplyParentURI(pv *bsky.FeedDefs_PostView) string {
	return extractReplyParentURI(pv)
}

// ShortURI returns a compact representation of an at:// post URI (did/postid) or the original string.
func ShortURI(uri string) string {
	if uri == "" {
		return ""
	}
	// expected form: at://did/app.bsky.feed.post/postid
	if strings.HasPrefix(uri, "at://") {
		parts := strings.Split(strings.TrimPrefix(uri, "at://"), "/")
		if len(parts) >= 3 {
			// parts[0]=did, parts[1]=app.bsky.feed.post, parts[2]=postid
			return parts[0] + "/" + parts[len(parts)-1]
		}
	}
	return uri
}

// ParentInfo captures lightweight parent details available from a ReplyRef without fetching the parent post.
type ParentInfo struct {
	AuthorName   string
	AuthorHandle string
	Text         string
	Uri          string
	Avatar       string
	PostURL      string
	IndexedAt    string
	Media        *MediaVM
	// whether the signed-in viewer has liked this post (from PostView.Viewer.Like)
	IsFav bool
	// like count for the parent post (populated by handlers from PostView.LikeCount)
	LikeCount   int
	ReplyCount  int
	RepostCount int
}

// GetParentInfo extracts whatever metadata is present in the ReplyRef.Parent or ReplyRef.Root
// using concrete, type-safe assertions (no reflection). It prefers Parent over Root and
// only extracts the Uri when available from known concrete types.
func GetParentInfo(pv *bsky.FeedDefs_PostView) ParentInfo {
	pi := ParentInfo{}
	if pv == nil || pv.Record == nil || pv.Record.Val == nil {
		return pi
	}
	post, ok := pv.Record.Val.(*bsky.FeedPost)
	if !ok || post == nil || post.Reply == nil {
		return pi
	}
	// prefer Parent over Root
	var ref interface{}
	if post.Reply.Parent != nil {
		ref = post.Reply.Parent
	} else if post.Reply.Root != nil {
		ref = post.Reply.Root
	}
	if ref == nil {
		return pi
	}
	// set Uri if available via existing helper
	pi.Uri = extractReplyParentURI(pv)

	// Try known concrete types (atproto.RepoStrongRef) to extract Uri
	if sr, ok := ref.(*atproto.RepoStrongRef); ok {
		if sr.Uri != "" {
			pi.Uri = sr.Uri
		}
		return pi
	}

	// If other concrete types are introduced by the API, avoid reflection and return what we have.
	return pi
}

// GetReplyChainInfos extracts available reply-ref metadata from a PostView without performing network fetches.
// It returns a slice of ParentInfo ordered from root (top-most ancestor) to immediate parent.
// This implementation is type-safe and only uses concrete types; it will populate Uri when available.
// NOTE: Reply refs carry only lightweight references (Uri/Cid). To display author handles, display names
// and text previews for ancestors, handlers should collect all referenced URIs and call fetchPostsBatch
// once to obtain full PostView objects, then populate a ParentInfo map passed into templates. Helpers
// must not perform network I/O (per project rules), so this function intentionally avoids fetching.
func GetReplyChainInfos(pv *bsky.FeedDefs_PostView) []ParentInfo {
	var out []ParentInfo
	if pv == nil || pv.Record == nil || pv.Record.Val == nil {
		return out
	}
	post, ok := pv.Record.Val.(*bsky.FeedPost)
	if !ok || post == nil || post.Reply == nil {
		return out
	}

	// helper to extract info from a reply-ref struct (root or parent)
	extract := func(ref interface{}) ParentInfo {
		pi := ParentInfo{}
		if ref == nil {
			return pi
		}
		// If the concrete type is a RepoStrongRef, extract Uri
		if sr, ok := ref.(*atproto.RepoStrongRef); ok {
			if sr.Uri != "" {
				pi.Uri = sr.Uri
			}
			return pi
		}
		// Unknown concrete type: avoid reflection and return empty
		return pi
	}

	// prefer root then parent to produce top-down order
	if post.Reply.Root != nil {
		rootInfo := extract(post.Reply.Root)
		out = append(out, rootInfo)
	}
	if post.Reply.Parent != nil {
		parentInfo := extract(post.Reply.Parent)
		// avoid duplicating the same Uri twice
		if !(len(out) > 0 && out[len(out)-1].Uri != "" && parentInfo.Uri != "" && out[len(out)-1].Uri == parentInfo.Uri) {
			out = append(out, parentInfo)
		}
	}
	return out
}

// GetEmbeddedParentInfo inspects a post's embed record (if it's an embedded record view)
// and returns a ParentInfo constructed from the embedded record's author and value fields.
// This is useful for rendering a quoted record as an ancestor in the chat-like UI when
// a full ReplyRef chain isn't available.
func GetEmbeddedParentInfo(pv *bsky.FeedDefs_PostView) ParentInfo {
	pi := ParentInfo{}
	if pv == nil || pv.Embed == nil {
		return pi
	}
	// Prefer embedded record view
	if pv.Embed.EmbedRecord_View != nil && pv.Embed.EmbedRecord_View.Record != nil {
		rw := pv.Embed.EmbedRecord_View.Record
		// the wrapped record may be an EmbedRecord_ViewRecord
		if rw.EmbedRecord_ViewRecord != nil {
			r := rw.EmbedRecord_ViewRecord
			// author: use existing helper to get a friendly display name
			if r.Author != nil {
				pi.AuthorName = getDisplayNameFromProfile(r.Author)
				// attempt to extract a handle from known concrete author types by converting to interface{}
				switch a := interface{}(r.Author).(type) {
				case *bsky.ActorDefs_ProfileView:
					pi.AuthorHandle = a.Handle
					if a.Avatar != nil {
						pi.Avatar = *a.Avatar
					}
				case *bsky.ActorDefs_ProfileViewBasic:
					pi.AuthorHandle = a.Handle
					if a.Avatar != nil {
						pi.Avatar = *a.Avatar
					}
				case *bsky.ActorDefs_ProfileViewDetailed:
					pi.AuthorHandle = a.Handle
					if a.Avatar != nil {
						pi.Avatar = *a.Avatar
					}
				default:
					// unknown author shape - leave handle/avatar empty
				}
			}
			// text/value: use getPostText which accepts *util.LexiconTypeDecoder
			if r.Value != nil {
				pi.Text = getPostText(r.Value)
			}
		}
	}
	return pi
}

// HasEmbedRecord reports whether the given FeedPost has an embedded record view
func HasEmbedRecord(item *bsky.FeedDefs_FeedViewPost) bool {
	if item == nil || item.Post == nil || item.Post.Embed == nil {
		return false
	}
	if item.Post.Embed.EmbedRecord_View != nil {
		return true
	}
	if item.Post.Embed.EmbedRecordWithMedia_View != nil {
		return true
	}
	return false
}

func IsReply(item *bsky.FeedDefs_FeedViewPost) bool {
	if item == nil || item.Post == nil {
		return false
	}
	return extractReplyParentURI(item.Post) != ""
}

func getIsFav(post *bsky.FeedDefs_PostView) bool {
	if post == nil || post.Viewer == nil || post.Viewer.Like == nil {
		return false
	}
	return len(*post.Viewer.Like) > 0
}
