package githubtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type httpServer struct {
	srv *httptest.Server
}

func (h *httpServer) url() string {
	// go-github's BaseURL must end in a slash; httptest URLs don't.
	return h.srv.URL + "/"
}

func (h *httpServer) close() {
	if h.srv != nil {
		h.srv.Close()
	}
}

func (s *Server) startHTTP() {
	mux := http.NewServeMux()

	// Boot-time endpoints: the dashboard hits these once at startup.
	mux.HandleFunc("/user", s.handleUser)
	mux.HandleFunc("/rate_limit", s.handleRateLimit)
	mux.HandleFunc("/repos/", s.routeRepos)
	mux.HandleFunc("/graphql", s.handleGraphQL)

	// OAuth flow + org-membership endpoints, used only by the oauth2-proxy
	// integration test. The rest of the suite bypasses these by stamping
	// X-Forwarded-User server-side. /user/orgs is what oauth2-proxy calls
	// to verify org membership when --github-org is set.
	mux.HandleFunc("/login/oauth/authorize", s.handleAuthorize)
	mux.HandleFunc("/login/oauth/access_token", s.handleAccessToken)
	mux.HandleFunc("/orgs/", s.handleOrgMember)
	mux.HandleFunc("/user/orgs", s.handleUserOrgs)
	mux.HandleFunc("/user/emails", s.handleUserEmails)
	mux.HandleFunc("/user/teams", s.handleUserTeams)

	// oauth2-proxy validates tokens with GET on its validate-url, which we
	// point at the fake's root. Real api.github.com returns 200 there, so
	// mirror that to let validation pass without disturbing the catch-all 404.
	mux.HandleFunc("GET /{$}", s.handleAPIRoot)
	mux.HandleFunc("/", s.handleNotFound)

	srv := httptest.NewServer(mux)
	s.httpx = &httpServer{srv: srv}
}

func (s *Server) close() {
	if s.httpx != nil {
		s.httpx.close()
	}
}

// routeRepos dispatches /repos/{owner}/{name}/... requests by path segment.
func (s *Server) routeRepos(w http.ResponseWriter, r *http.Request) {
	// /repos/{owner}/{name}                         (3) repo presence check
	// /repos/{owner}/{name}/pulls                   (4) list PRs
	// /repos/{owner}/{name}/pulls/{N}               (5) single PR
	// /repos/{owner}/{name}/pulls/{N}/reviews       (6) reviews
	// /repos/{owner}/{name}/commits/{sha}/check-runs(6) check runs
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "repos" {
		s.handleNotFound(w, r)
		return
	}
	owner, name := parts[1], parts[2]
	slug := owner + "/" + name

	switch {
	case len(parts) == 3:
		s.handleRepo(w, r, slug)
	case len(parts) == 4 && parts[3] == "pulls":
		s.handleListPulls(w, r, slug)
	case len(parts) == 5 && parts[3] == "pulls":
		s.handleGetPull(w, r, slug, parts[4])
	case len(parts) == 6 && parts[3] == "pulls" && parts[5] == "reviews":
		s.handleListReviews(w, r, slug, parts[4])
	case len(parts) == 6 && parts[3] == "commits" && parts[5] == "check-runs":
		s.handleListCheckRuns(w, r, slug, parts[4])
	default:
		s.handleNotFound(w, r)
	}
}

// handleAPIRoot serves GET /. oauth2-proxy hits this during token
// validation; an empty 200 is enough to signal the token works.
func (s *Server) handleAPIRoot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{})
}

// handleUser stubs token validation. A fake bearer token identifies a
// seeded user; the server PAT (or any unknown token) falls back to the
// fixed githubtest-bot identity the poller's startup relies on.
func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	if u, ok := s.authorizedUser(r); ok {
		writeJSON(w, http.StatusOK, jsonUser{
			Login:     u.Login,
			ID:        u.ID,
			AvatarURL: u.AvatarURL,
			Type:      "User",
		})
		return
	}
	writeJSON(w, http.StatusOK, jsonUser{
		Login: "githubtest-bot",
		ID:    1,
		Type:  "User",
	})
}

// handleRateLimit reports a healthy authenticated rate limit. A core
// limit of 60 reads as anonymous and logs an error, so report 5000.
func (s *Server) handleRateLimit(w http.ResponseWriter, _ *http.Request) {
	resp := jsonRateLimit{}
	resp.Resources.Core.Limit = 5000
	resp.Resources.Core.Remaining = 4999
	resp.Resources.Core.Used = 1
	resp.Resources.Core.Reset = time.Now().Add(time.Hour).Unix()
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRepo(w http.ResponseWriter, _ *http.Request, slug string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	repo, ok := s.repoJSON(slug)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) handleListPulls(w http.ResponseWriter, r *http.Request, slug string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := r.URL.Query().Get("state")
	prs := s.listPRsJSON(slug, state)
	writeJSON(w, http.StatusOK, prs)
}

func (s *Server) handleGetPull(w http.ResponseWriter, _ *http.Request, slug, numberStr string) {
	number, err := strconv.Atoi(numberStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid pull number"})
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.repos[slug]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "repo not found"})
		return
	}
	pr, ok := r.prs[number]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "pr not found"})
		return
	}
	writeJSON(w, http.StatusOK, s.prJSON(pr))
}

func (s *Server) handleListReviews(w http.ResponseWriter, _ *http.Request, slug, numberStr string) {
	number, err := strconv.Atoi(numberStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid pull number"})
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.repos[slug]
	if !ok {
		writeJSON(w, http.StatusOK, []jsonReview{})
		return
	}
	pr, ok := r.prs[number]
	if !ok {
		writeJSON(w, http.StatusOK, []jsonReview{})
		return
	}
	writeJSON(w, http.StatusOK, s.reviewsJSON(pr))
}

func (s *Server) handleListCheckRuns(w http.ResponseWriter, _ *http.Request, slug, sha string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.repos[slug]
	if !ok {
		writeJSON(w, http.StatusOK, jsonCheckRunsList{TotalCount: 0, CheckRuns: []jsonCheckRun{}})
		return
	}
	// Check runs are scoped to (repo, head_sha), so match on head SHA.
	for _, pr := range r.prs {
		if pr.HeadSHA == sha {
			writeJSON(w, http.StatusOK, s.checksJSON(pr))
			return
		}
	}
	writeJSON(w, http.StatusOK, jsonCheckRunsList{TotalCount: 0, CheckRuns: []jsonCheckRun{}})
}

// graphQLOwnerNameRE pulls owner/name from the rollupQuery's variables.
// The dashboard sends exactly one operation, so a regex beats a full parse.
var graphQLOwnerNameRE = regexp.MustCompile(`(?s)"variables".*?"owner"\s*:\s*"([^"]+)".*?"name"\s*:\s*"([^"]+)"`)

func (s *Server) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "read body"})
		return
	}
	m := graphQLOwnerNameRE.FindSubmatch(body)
	if m == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "missing owner/name in graphql variables"})
		return
	}
	slug := fmt.Sprintf("%s/%s", m[1], m[2])
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, http.StatusOK, s.graphQLRollup(slug))
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	// Loud 404 so a missing endpoint stands out instead of being
	// swallowed by go-github's retry logic.
	s.logf("githubtest: unhandled %s %s", r.Method, r.URL.Path)
	writeJSON(w, http.StatusNotFound, map[string]string{
		"message": fmt.Sprintf("githubtest: unhandled %s %s", r.Method, r.URL.Path),
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Header's already sent, so we can't recover; just log it. Rare,
		// but catches scenarios that produce non-marshalable types.
		fmt.Fprintf(os.Stderr, "githubtest: encode response %T: %v\n", v, err)
	}
}
