package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/bluesky-social/indigo/api/atproto"
)

// handleVideo serves video blobs. Preferred path: fetch the blob from the user's
// PDS via com.atproto.sync.getBlob (SyncGetBlob) using the authenticated API client
// (getClientFromSession). This returns the raw blob bytes which we serve directly
// and implement Range support by slicing the blob. If that fails (no session or
// error), fall back to proxying a public IPFS gateway (legacy behavior).
func handleVideo(w http.ResponseWriter, r *http.Request) {
	// Support URL shapes: /video/{cid} (legacy) and /video/{did}/{cid} (owner-aware)
	path := strings.TrimPrefix(r.URL.Path, "/video/")
	path = strings.TrimSpace(path)
	if path == "" {
		http.Error(w, "missing video id", http.StatusBadRequest)
		return
	}
	segments := strings.SplitN(path, "/", 2)
	ownerDid := ""
	cid := ""
	if len(segments) == 1 {
		cid = segments[0]
	} else {
		ownerDid = segments[0]
		cid = segments[1]
	}

	// Try to get authenticated client and session DID from session
	c, sessionDid, err := getClientFromSession(r.Context(), r)
	var blob []byte
	if err == nil {
		// Determine which DID to pass to SyncGetBlob: prefer explicit ownerDid from URL
		didToUse := sessionDid
		if ownerDid != "" {
			didToUse = ownerDid
		}
		// Attempt to fetch the blob via AT Protocol sync.getBlob
		blob, err = atproto.SyncGetBlob(context.Background(), c, cid, didToUse)
		if err != nil {
			log.Printf("DEBUG: handleVideo - SyncGetBlob error for cid=%s did=%s: %v", cid, didToUse, err)
			blob = nil
		}
	} else {
		log.Printf("DEBUG: handleVideo - no authenticated session: %v", err)
	}

	// If we have the blob bytes from the PDS, serve it (with Range support)
	if blob != nil {
		size := int64(len(blob))
		w.Header().Set("Accept-Ranges", "bytes")
		// Try to honor client's Range header
		rh := r.Header.Get("Range")
		// Default content-type fallback
		w.Header().Set("Content-Type", "video/mp4")

		if rh == "" {
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
			w.WriteHeader(http.StatusOK)
			if _, err := io.Copy(w, bytes.NewReader(blob)); err != nil {
				log.Printf("DEBUG: handleVideo - error streaming blob cid=%s: %v", cid, err)
			}
			return
		}

		// Parse single-range header of form: bytes=start-end
		if !strings.HasPrefix(rh, "bytes=") {
			http.Error(w, "invalid range", http.StatusBadRequest)
			return
		}
		rangeSpec := strings.TrimPrefix(rh, "bytes=")
		parts := strings.Split(rangeSpec, "-")
		if len(parts) != 2 {
			http.Error(w, "invalid range", http.StatusBadRequest)
			return
		}

		start, err1 := strconv.ParseInt(parts[0], 10, 64)
		var end int64 = size - 1
		var err2 error
		if parts[1] != "" {
			end, err2 = strconv.ParseInt(parts[1], 10, 64)
			if err2 != nil {
				end = size - 1
			}
		}
		if err1 != nil || start < 0 || start > end || start >= size {
			// RFC 7233: respond 416 Range Not Satisfiable and include Content-Range */size
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			http.Error(w, "Requested Range Not Satisfiable", http.StatusRequestedRangeNotSatisfiable)
			return
		}

		if end >= size {
			end = size - 1
		}
		length := end - start + 1
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(http.StatusPartialContent)
		if _, err := io.Copy(w, bytes.NewReader(blob[start:end+1])); err != nil {
			log.Printf("DEBUG: handleVideo - error streaming ranged blob cid=%s: %v", cid, err)
		}
		return
	}

	// Fallback: proxy to public IPFS gateway (existing behavior)
	gateway := "https://dweb.link/ipfs/"
	url := gateway + cid

	// Build request to gateway. Forward Range header if present so gateway can respond with 206.
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("DEBUG: handleVideo - new request error for %s: %v", url, err)
		http.Error(w, "failed to fetch media", http.StatusBadGateway)
		return
	}
	if rh := r.Header.Get("Range"); rh != "" {
		req.Header.Set("Range", rh)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("DEBUG: handleVideo - error fetching %s: %v", url, err)
		http.Error(w, "failed to fetch media", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		log.Printf("DEBUG: handleVideo - gateway returned status %d for %s", resp.StatusCode, url)
		if resp.StatusCode == http.StatusNotFound {
			http.Error(w, "media not found", http.StatusNotFound)
		} else {
			http.Error(w, "failed to fetch media", http.StatusBadGateway)
		}
		return
	}

	// Forward relevant headers
	w.Header().Set("Accept-Ranges", "bytes")
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "video/mp4")
	}
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		w.Header().Set("Content-Range", cr)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}

	// Mirror status code (200 or 206)
	w.WriteHeader(resp.StatusCode)

	// Stream response body directly to client
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("DEBUG: handleVideo - error streaming body for %s: %v", url, err)
	}
}
