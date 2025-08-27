// Centralized application JavaScript for Tuiter 2006
// This file replaces inline scripts previously embedded in templates.

(function(){
  'use strict';

  // Char count updater for post box
  function updateCharCount(remaining){
    var charCountEl = document.getElementById('char-count');
    if (charCountEl) {
      charCountEl.textContent = remaining;
      charCountEl.style.color = remaining < 20 ? 'red' : (remaining < 50 ? 'orange' : 'green');
    }
  }

  // Expose to global for compatibility
  window.updateCharCount = updateCharCount;

  // Form wiring for post-box forms: update char count and reset on htmx:afterRequest
  function initPostBoxForms(){
    var forms = document.querySelectorAll('.post-box-form');
    forms.forEach(function(form){
      var ta = form.querySelector('textarea[data-maxlength]');
      var max = ta && parseInt(ta.getAttribute('data-maxlength'), 10) || 140;
      if (ta){
        ta.addEventListener('input', function(){ updateCharCount(max - this.value.length); });
        // set initial
        updateCharCount(max - ta.value.length);
      }

      // Listen for HTMX afterRequest to reset the form
      form.addEventListener('htmx:afterRequest', function(evt){
        try{ form.reset(); if (ta) updateCharCount(max); } catch(e){ console.log('form reset error', e); }
      });
    });
  }

  // Lightbox handling (moved from footer template)
  function initLightbox(){
    var overlay = document.getElementById('lightbox-overlay');
    if (!overlay) return;
    var img = document.getElementById('lightbox-img');
    var video = document.getElementById('lightbox-video');

    function showOverlay(){ overlay.classList.add('visible'); overlay.setAttribute('aria-hidden','false'); }
    function hideOverlay(){ overlay.classList.remove('visible'); overlay.setAttribute('aria-hidden','true'); }

    function openImage(src, alt){
      try{ video.pause(); } catch(e){}
      video.removeAttribute('src');
      while(video.firstChild) video.removeChild(video.firstChild);
      try{ video.load(); } catch(e){}
      video.style.display = 'none';

      img.src = src;
      img.alt = alt || '';
      img.style.display = '';
      showOverlay();
    }

    function openVideo(src, mime){
      img.src = '';
      img.alt = '';
      img.style.display = 'none';

      var type = mime || 'video/mp4';
      while(video.firstChild) video.removeChild(video.firstChild);
      var source = document.createElement('source');
      source.src = src;
      source.type = type;
      video.appendChild(source);

      video.style.display = '';
      try{ video.load(); var p = video.play(); if (p && typeof p.then === 'function') p.catch(function(){}); } catch(e){ console.log('DEBUG: video play error', e); }
      showOverlay();
    }

    function closeLightbox(){
      try{ video.pause(); } catch(e){}
      video.removeAttribute('src');
      while(video.firstChild) video.removeChild(video.firstChild);
      try{ video.load(); } catch(e){}

      img.src = '';
      img.alt = '';
      img.style.display = '';
      hideOverlay();
    }

    document.addEventListener('click', function(e){
      var t = e.target;
      if (!t || !t.classList) return;

      if (t.classList.contains('post-image')){
        e.preventDefault();
        var parent = t.closest('a');
        var href = parent && parent.getAttribute('href');
        if (href) openImage(href, t.getAttribute('alt'));
        return;
      }

      if (t.classList.contains('post-video-thumb')){
        e.preventDefault();
        var parent = t.closest('a');
        var href = parent && parent.getAttribute('href');
        var mime = parent && parent.dataset && parent.dataset.mime;
        if (href) openVideo(href, mime || 'video/mp4');
        return;
      }

      if (t.id === 'lightbox-overlay') closeLightbox();
    }, false);

    document.addEventListener('keydown', function(e){ if (e.key === 'Escape') closeLightbox(); });
  }

  // Initialize banner backgrounds set via data attributes
  function initProfileBanners(){
    var nodes = document.querySelectorAll('[data-banner-url]');
    nodes.forEach(function(n){
      var url = n.getAttribute('data-banner-url');
      if (url && url !== '') n.style.backgroundImage = "url('" + url + "')";
    });
  }

  // Setup video element styling for embedded videos
  function initVideoStyling(){
    var vids = document.querySelectorAll('.video-embedded');
    vids.forEach(function(v){
      v.style.maxWidth = '100%';
      v.style.height = 'auto';
      v.style.background = '#000';
    });

    var lightboxVideo = document.getElementById('lightbox-video');
    if (lightboxVideo){ lightboxVideo.style.maxWidth = '100%'; lightboxVideo.style.maxHeight = '80vh'; lightboxVideo.style.display = 'none'; }
  }

  // Post page initialization: auto-scroll highlighted post and toggle flat/nested
  function initPostPage(){
    // Auto-scroll to highlighted post
    var highlighted = document.querySelector('.highlighted-post');
    if (highlighted) {
      highlighted.scrollIntoView({behavior: 'smooth', block: 'center'});
      highlighted.style.transition = 'box-shadow 0.4s ease';
      highlighted.style.boxShadow = '0 0 0 3px rgba(255,204,51,0.6)';
      setTimeout(function(){ highlighted.style.boxShadow = ''; }, 2000);
    }

    // Toggle flat/nested views
    (function() {
      var flatBtn = document.getElementById('toggle-flat');
      var nestedBtn = document.getElementById('toggle-nested');
      var replies = document.getElementById('replies-container');
      if (!flatBtn || !nestedBtn || !replies) return;
      flatBtn.addEventListener('click', function(){
        flatBtn.classList.add('active');
        nestedBtn.classList.remove('active');
        replies.classList.add('flat-view');
      });
      nestedBtn.addEventListener('click', function(){
        nestedBtn.classList.add('active');
        flatBtn.classList.remove('active');
        replies.classList.remove('flat-view');
      });
    })();
  }

  // Reply input handling: create a slim chat-like reply input under a post or chat bubble
  function closeOpenReplyInput(){
    var ex = document.querySelector('.reply-input-container.absolute');
    if (ex && ex.parentNode) ex.parentNode.removeChild(ex);
    // remove transient listeners if any
    try{ window.removeEventListener('scroll', closeOpenReplyInput); window.removeEventListener('resize', closeOpenReplyInput); } catch(e){}
  }

  function createReplyInput(refEl, insertAfterEl){
    // refEl is typically the clicked reply-button element; insertAfterEl is optional contextual element
    if (!refEl) return null;
    // Close any existing reply input
    closeOpenReplyInput();

    var container = document.createElement('div');
    container.className = 'reply-input-container absolute active';
    container.setAttribute('role','region');
    container.setAttribute('aria-label','Reply input');

    // Try to obtain signed-in avatar from the page-level container data attribute
    var siteContainer = document.querySelector('.container');
    var avatarUrl = siteContainer && siteContainer.dataset && siteContainer.dataset.signedInAvatar ? siteContainer.dataset.signedInAvatar : '';

    // Avatar square (if available) or placeholder
    if (avatarUrl && avatarUrl !== ''){
      try{
        var avatarEl = document.createElement('img');
        avatarEl.className = 'reply-avatar-square';
        avatarEl.src = avatarUrl;
        avatarEl.alt = 'Your avatar';
        avatarEl.setAttribute('aria-hidden','true');
        container.appendChild(avatarEl);
      } catch(e){ /* ignore image construction errors */ }
    } else {
      var ph = document.createElement('div');
      ph.className = 'reply-avatar-placeholder';
      ph.setAttribute('aria-hidden','true');
      container.appendChild(ph);
    }

    var input = document.createElement('input');
    input.type = 'text';
    input.className = 'reply-input';
    input.setAttribute('placeholder', 'Write a reply...');
    input.setAttribute('aria-label', 'Write a reply');

    var btn = document.createElement('button');
    btn.className = 'reply-submit';
    btn.type = 'button';
    btn.textContent = 'Reply';
    // No-op click handler for now, keep a debug log
    btn.addEventListener('click', function(ev){ ev.preventDefault(); try{ console.debug('Reply button clicked (no-op)'); } catch(e){} });

    container.appendChild(input);
    container.appendChild(btn);

    // Append to body to avoid layout shifts
    document.body.appendChild(container);

    // Compute position based on refEl (reply-button) bounding rect and available space
    try{
      var rbRect = refEl.getBoundingClientRect();
      var containerRect = siteContainer ? siteContainer.getBoundingClientRect() : { left: 8, width: Math.min(window.innerWidth - 16, 1000) };

      // desired width: try to fit inside main container, account for avatar column (~44px)
      var desiredWidth = Math.min(760, Math.max(320, Math.floor(containerRect.width - 56)));

      // center the input around the reply button horizontally but keep it inside viewport/container
      var left = window.scrollX + Math.max(containerRect.left + 12, Math.min(rbRect.left + window.scrollX - (desiredWidth/2) + (rbRect.width/2), containerRect.left + window.scrollX + containerRect.width - desiredWidth - 12));
      var top = window.scrollY + rbRect.top + rbRect.height + 8; // place just below the button

      container.style.position = 'absolute';
      container.style.left = left + 'px';
      container.style.top = top + 'px';
      container.style.width = desiredWidth + 'px';
      container.style.zIndex = 9999;
    } catch(e){ console.debug('reply input positioning error', e); }

    // Focus the input for convenience
    try{ input.focus(); } catch(e){}

    // Close when user scrolls or resizes to avoid floating orphaned inputs
    try{ window.addEventListener('scroll', closeOpenReplyInput, { passive: true }); window.addEventListener('resize', closeOpenReplyInput); } catch(e){}

    return container;
  }

  function initReplyButtons(){
    // Toggle reply input when a reply button is clicked. Use event delegation.
    document.addEventListener('click', function(e){
      var t = e.target;
      if (!t || !t.closest) return;

      // If a reply-button (or child) was clicked
      var rb = t.closest('.reply-button');
      if (!rb) return;
      e.preventDefault();

      // Find contextual element for width calculation (prefer post-content, then chat-bubble)
      var postContent = rb.closest('.post-content');
      var chatBubble = rb.closest('.chat-bubble');
      var chatNode = rb.closest('.chat-node');
      var post = rb.closest('.post');

      var contextEl = postContent || chatBubble || chatNode || post;

      // If there's already a reply input visible, toggle it closed
      var existing = document.querySelector('.reply-input-container.absolute');
      if (existing){
        closeOpenReplyInput();
        // If the existing was opened for the same reply-button, stop here (toggle)
        return;
      }

      createReplyInput(rb, contextEl);
    }, false);

    // Close the reply input when clicking outside of it or a reply-button
    document.addEventListener('click', function(e){
      var t = e.target;
      if (!t) return;
      if (t.closest && (t.closest('.reply-input-container') || t.closest('.reply-button'))) return;
      closeOpenReplyInput();
    }, false);
  }

  // On DOM ready
  document.addEventListener('DOMContentLoaded', function(){
    initLightbox();
    initProfileBanners();
    initVideoStyling();
    initPostBoxForms();

    // ensure initial char count reflects current textarea
    var ta = document.getElementById('status-input');
    if (ta) updateCharCount(140 - ta.value.length);

    // init post page behaviour if present
    initPostPage();

    // initialize reply button behaviour
    initReplyButtons();
  });

  // expose initPostPage for compatibility with small inline stub
  window.initPostPage = initPostPage;

})();
