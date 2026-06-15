package githubtest

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// FakeUser is the GitHub-side identity the fake treats as signed in.
// /authorize stamps tokens for this user; /user reports its details when
// called with the matching bearer token.
type FakeUser struct {
	Login     string
	ID        int64
	AvatarURL string
}

// defaultFakeUser is the signed-in identity before any test calls AsUser,
// so identity-agnostic tests get a valid default-org member for free.
var defaultFakeUser = FakeUser{
	Login:     "alex",
	ID:        1001,
	AvatarURL: "https://avatars.githubusercontent.com/u/1001",
}

// Synthetic tokens for the fake OAuth flow. The login is appended verbatim
// so /access_token and /user recover the identity with no bookkeeping. Not
// credentials.
//
//nolint:gosec // hardcoded test fixtures, never used outside the fake server.
const (
	fakeCodePrefix        = "fake-code-"
	fakeAccessTokenPrefix = "fake-user-token-"
)

// AsUser sets the identity the fake returns on the next OAuth login. Tests
// call it in setup; the harness calls it from WithViewer so scenarios get
// the right current user automatically.
func (s *Server) AsUser(u FakeUser) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentUser = u
}

// CurrentUser returns the user the fake is currently set to sign in
// as. Useful for assertions in the OAuth tests.
func (s *Server) CurrentUser() FakeUser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentUser
}

// AddOrgMember registers username in org. /orgs/{org}/members/{user}
// returns 204 for pairs added here, 404 otherwise.
func (s *Server) AddOrgMember(org, username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orgMembers == nil {
		s.orgMembers = map[string]map[string]struct{}{}
	}
	if _, ok := s.orgMembers[org]; !ok {
		s.orgMembers[org] = map[string]struct{}{}
	}
	s.orgMembers[org][username] = struct{}{}
}

// RemoveOrgMember undoes AddOrgMember. Used by the periodic-recheck
// test to simulate a user being removed from the org mid-session.
func (s *Server) RemoveOrgMember(org, username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.orgMembers[org]; ok {
		delete(m, username)
	}
}

// isOrgMember is the lookup the /orgs/{org}/members/{user} handler
// performs. Caller must hold s.mu (read or write).
func (s *Server) isOrgMember(org, username string) bool {
	m, ok := s.orgMembers[org]
	if !ok {
		return false
	}
	_, ok = m[username]
	return ok
}

// handleAuthorize implements OAuth /login/oauth/authorize. Real GitHub
// shows a consent page; the fake skips it and 302s straight to redirect_uri
// with a stamped code and the caller's state echoed back.
func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	if redirectURI == "" {
		http.Error(w, "missing redirect_uri", http.StatusBadRequest)
		return
	}

	target, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	login := s.currentUser.Login
	s.mu.RUnlock()
	if login == "" {
		login = defaultFakeUser.Login
	}

	tq := target.Query()
	tq.Set("code", fakeCodePrefix+login)
	tq.Set("state", state)
	target.RawQuery = tq.Encode()

	// gosec flags this as open-redirect-ish since redirect_uri is
	// caller-supplied. Real GitHub pre-registers it; the fake accepts
	// whatever the dashboard sends, since tests configure both sides.
	http.Redirect(w, r, target.String(), http.StatusFound) //nolint:gosec // test fake; redirect target is dashboard-supplied
}

// handleAccessToken implements OAuth POST /login/oauth/access_token. Called
// with the code stamped in /authorize; returns a bearer token derived from
// the same login so /user can identify the caller statelessly.
func (s *Server) handleAccessToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	code := r.Form.Get("code")
	if !strings.HasPrefix(code, fakeCodePrefix) {
		http.Error(w, "invalid code", http.StatusBadRequest)
		return
	}
	login := strings.TrimPrefix(code, fakeCodePrefix)

	resp := map[string]string{
		"access_token": fakeAccessTokenPrefix + login,
		"token_type":   "bearer",
		"scope":        "read:org",
	}

	// x/oauth2 accepts form-encoded or JSON; we use form-encoded, the
	// legacy GitHub default.
	w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
	w.WriteHeader(http.StatusOK)
	form := url.Values{}
	for k, v := range resp {
		form.Set(k, v)
	}
	_, _ = fmt.Fprint(w, form.Encode())
}

// authorizedUser parses the Authorization header for the fake bearer-token
// format and returns the matching FakeUser. Returns ok=false when the
// header is missing or carries the server PAT, leaving poller tests alone.
func (s *Server) authorizedUser(r *http.Request) (FakeUser, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") && !strings.HasPrefix(h, "token ") {
		return FakeUser{}, false
	}
	tok := strings.TrimPrefix(strings.TrimPrefix(h, "Bearer "), "token ")
	if !strings.HasPrefix(tok, fakeAccessTokenPrefix) {
		return FakeUser{}, false
	}
	login := strings.TrimPrefix(tok, fakeAccessTokenPrefix)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.currentUser.Login == login {
		return s.currentUser, true
	}
	// Token doesn't match the active fake user (can happen mid-switch);
	// synthesize a user from the token's login so the handler stays coherent.
	return FakeUser{
		Login:     login,
		ID:        deriveFakeID(login),
		AvatarURL: fmt.Sprintf("https://avatars.githubusercontent.com/u/%d", deriveFakeID(login)),
	}, true
}

// deriveFakeID hashes a login to a stable small int64, so "bob" gets the
// same ID every run and IDs stay readable when eyeballing tables.
func deriveFakeID(login string) int64 {
	if login == "" {
		return defaultFakeUser.ID
	}
	var h int64 = 5381
	for _, b := range []byte(login) {
		h = h*33 + int64(b)
	}
	if h < 0 {
		h = -h
	}
	// Keep the small id range free for hand-picked test users so hashed
	// IDs don't collide with explicit ones.
	return 100000 + (h % 1_000_000_000)
}

// handleOrgMember serves /orgs/{org}/members/{username}: 204 for a member,
// 404 otherwise, looked up against the seeded set.
func (s *Server) handleOrgMember(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	// /orgs/{org}/members/{username} is 4 segments.
	if len(parts) != 4 || parts[0] != "orgs" || parts[2] != "members" {
		s.handleNotFound(w, r)
		return
	}
	org := parts[1]
	username := parts[3]

	s.mu.RLock()
	member := s.isOrgMember(org, username)
	s.mu.RUnlock()
	if member {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}

// handleUserTeams serves GET /user/teams. oauth2-proxy calls this even
// without --github-team. We don't model teams, so return an empty list
// (page > 1 returns [] too, same as /user/orgs).
func (s *Server) handleUserTeams(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizedUser(r); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Requires authentication"})
		return
	}
	writeJSON(w, http.StatusOK, []struct{}{})
}

// handleUserEmails serves GET /user/emails. oauth2-proxy calls this to
// resolve the primary email when --email-domain is set; we synthesize one
// verified-primary entry from the login so --email-domain=* is happy.
func (s *Server) handleUserEmails(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authorizedUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Requires authentication"})
		return
	}
	type entry struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	writeJSON(w, http.StatusOK, []entry{{
		Email:    u.Login + "@example.com",
		Primary:  true,
		Verified: true,
	}})
}

// handleUserOrgs serves GET /user/orgs, returning the orgs the caller was
// seeded into via AddOrgMember (oauth2-proxy matches them against --github-org).
//
// Real GitHub paginates via the Link header and oauth2-proxy follows it
// until rel="next" vanishes; without that it would loop forever. So we serve
// everything on page 1 and return [] for any page > 1.
func (s *Server) handleUserOrgs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authorizedUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Requires authentication"})
		return
	}

	type jsonOrg struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	}

	page := r.URL.Query().Get("page")
	if page != "" && page != "1" {
		writeJSON(w, http.StatusOK, []jsonOrg{})
		return
	}

	s.mu.RLock()
	var orgs []jsonOrg
	for org, members := range s.orgMembers {
		if _, ok := members[u.Login]; ok {
			orgs = append(orgs, jsonOrg{Login: org, ID: deriveFakeID(org)})
		}
	}
	s.mu.RUnlock()

	if orgs == nil {
		orgs = []jsonOrg{}
	}
	writeJSON(w, http.StatusOK, orgs)
}
