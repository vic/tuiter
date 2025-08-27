package main

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/gorilla/sessions"
)

const (
	sessionName = "twitter-2006-session"
)

//go:embed templates/*
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

var (
	oauthApp *oauth.ClientApp
	store    *sessions.CookieStore
	tpl      *template.Template
)

// Run initializes global state and starts the HTTP server.
func Run() {
	config := oauth.NewPublicConfig(
		os.Getenv("BSKY_CLIENT_ID"),
		os.Getenv("BSKY_REDIRECT_URI"),
		[]string{"atproto", "transition:generic"},
	)
	oauthApp = oauth.NewClientApp(&config, oauth.NewMemStore())

	key := os.Getenv("SESSION_DB_KEY")
	if key == "" {
		log.Fatal("SESSION_DB_KEY environment variable not set")
	}

	derivedKey, err := deriveKeyFromEnv(key)
	if err != nil {
		log.Fatalf("failed to derive key from SESSION_DB_KEY: %v", err)
	}

	store = sessions.NewCookieStore([]byte(os.Getenv("SESSION_SECRET")))
	store.Options = &sessions.Options{HttpOnly: true, Secure: false, Path: "/"}

	var dbPath string
	if v := os.Getenv("SESSION_DB_PATH"); v != "" {
		dbPath = v
	}
	sqliteStore, err := NewSQLiteStore(dbPath, derivedKey)
	if err != nil {
		log.Fatalf("failed to initialize SQLite store: %v", err)
	}
	oauthApp.Store = sqliteStore

	funcMap := template.FuncMap{
		"getPostText":         getPostText,
		"getProfileURL":       getProfileURL,
		"getPostURL":          getPostURL,
		"getFollowingCount":   getFollowingCount,
		"getFollowersCount":   getFollowersCount,
		"getPostsCount":       getPostsCount,
		"getDisplayName":      getDisplayNameFromProfile,
		"getCursor":           func(t interface{}) string { return getCursorFromAny(t) },
		"getPostPrefix":       GetPostPrefix,
		"getEmbedRecord":      GetEmbedRecord,
		"embedContext":        embedContext,
		"getPostMedia":        GetPostMedia,
		"getMediaForTemplate": GetMediaForTemplate,
		"makeElementID":       MakeElementID,
		"wrapThread":          wrapThread,
		// newly added helpers
		"avatarURL":          AvatarURL,
		"AvatarURL":          AvatarURL,
		"hasAvatar":          HasAvatar,
		"bannerURL":          BannerURL,
		"postBoxInitial":     PostBoxInitial,
		"postBoxPlaceholder": PostBoxPlaceholder,
		"isPostRetweet":      IsPostRetweet,
		"isPostQuote":        IsPostQuote,
		"buildPostVM":        buildPostVMForTemplate,
		"hasItems":           HasItems,
		// reply helpers
		"isPostReply":           IsPostReply,
		"replyParentURI":        ReplyParentURI,
		"shortURI":              ShortURI,
		"getParentInfo":         GetParentInfo,
		"getReplyChainInfos":    GetReplyChainInfos,
		"getEmbeddedParentInfo": GetEmbeddedParentInfo,
		"hasEmbedRecord":        HasEmbedRecord,
		"isReply":               IsReply,
		// helper to build small maps in templates
		"dict": func(vals ...interface{}) map[string]interface{} {
			m := make(map[string]interface{})
			for i := 0; i < len(vals); i += 2 {
				k, _ := vals[i].(string)
				if i+1 < len(vals) {
					m[k] = vals[i+1]
				}
			}
			return m
		},
		"getIsFav": getIsFav,
		// expose like counts to templates
		"getLikeCount": getLikeCount,
	}

	tpl = template.Must(template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.html"))

	subStaticFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("failed to prepare static filesystem: %v", err)
	}

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/signin", handleSignin)
	http.HandleFunc("/post-status", handlePostStatus)
	http.HandleFunc("/login", handleLogin)
	http.HandleFunc("/logout", handleLogout)
	http.HandleFunc("/oauth-callback", handleOAuthCallback)
	http.HandleFunc("/oauth-client-metadata.json", handleClientMetadata)
	http.HandleFunc("/timeline", handleTimeline)
	http.HandleFunc("/timeline/post", handleTimelinePost)
	http.HandleFunc("/post/", handlePost)
	http.HandleFunc("/profile/", handleProfile)
	http.HandleFunc("/reply", handleReply)
	http.HandleFunc("/htmx/timeline", htmxTimelineFeed)
	http.HandleFunc("/htmx/profile", htmxProfileFeed)
	http.HandleFunc("/video/", handleVideo)
	http.HandleFunc("/about", handleAbout)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(subStaticFS))))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Println("Listening on http://localhost:" + port)
	log.Fatal(http.ListenAndServe(":"+port, loggingMiddleware(http.DefaultServeMux)))
}
