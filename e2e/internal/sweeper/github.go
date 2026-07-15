package sweeper

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// GitHubRunChecker returns a RunChecker that consults the GitHub Actions API to
// see whether a workflow run id has reached the "completed" status. It is used
// by `sweep --mode finished-run` so only resources owned by a finished run are
// deleted, avoiding the TTL self-deletion race.
func GitHubRunChecker(repo, token string) RunChecker {
	client := &http.Client{Timeout: 15 * time.Second}
	cache := map[string]bool{}
	return func(runID string) (bool, error) {
		if v, ok := cache[runID]; ok {
			return v, nil
		}
		url := fmt.Sprintf("https://api.github.com/repos/%s/actions/runs/%s", repo, runID)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return false, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return false, err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			// Unknown run id — treat as finished so stale resources can be reaped.
			cache[runID] = true
			return true, nil
		}
		if resp.StatusCode != http.StatusOK {
			return false, fmt.Errorf("github api: %s", resp.Status)
		}
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return false, err
		}
		finished := body.Status == "completed"
		cache[runID] = finished
		return finished, nil
	}
}
