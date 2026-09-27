package pocketkit

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// diagnoseAccess explains why no release was found.
//
// GitHub returns 404 for both missing releases and inaccessible private repos.
// Probe repository access to add context without replacing the original error.
func diagnoseAccess(ctx context.Context, slug, token string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/"+slug, http.NoBody)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("\n\nCould not reach the GitHub API to say why: %v", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return "\n\nThe repository is reachable with these credentials. " +
			"See the original error above for the failure."

	case http.StatusNotFound:
		if token == "" {
			return "\n\nGitHub returned 404. If this repository is private that is " +
				"what an unauthenticated request gets -- pass --token or set GITHUB_TOKEN."
		}
		return "\n\nGitHub returned 404 even with a token. The token may lack access " +
			"to this repository, or the repository name may be wrong."

	case http.StatusUnauthorized:
		return "\n\nGitHub rejected the token (401). It may be expired or malformed."

	case http.StatusForbidden:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return "\n\nGitHub rate limit reached (403). A token raises the limit."
		}
		return "\n\nGitHub refused the request (403). If your organisation enforces " +
			"SAML SSO, the token may need to be authorised for it."

	default:
		return fmt.Sprintf("\n\nGitHub returned %s when asked about %s.", resp.Status, slug)
	}
}
