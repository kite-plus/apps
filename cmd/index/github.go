package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"browser_download_url"`
	// Digest is "sha256:<hex>" for assets uploaded since GitHub began
	// recording one, and empty before.
	Digest string `json:"digest"`
}

// github reads releases and downloads their archives. A token raises the
// API's limit from 60 requests an hour; none is needed to read public
// repositories.
type github struct {
	client *http.Client
	api    string
	token  string
}

func newGitHub(token string) *github {
	return &github{client: &http.Client{Timeout: 2 * time.Minute}, api: "https://api.github.com", token: token}
}

// releases lists a repository's published releases, newest first, leaving
// out drafts and pre-releases.
func (g *github) releases(ctx context.Context, repo string) ([]ghRelease, error) {
	var out []ghRelease
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/repos/%s/releases?per_page=100&page=%d", g.api, repo, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if g.token != "" {
			req.Header.Set("Authorization", "Bearer "+g.token)
		}
		resp, err := g.client.Do(req)
		if err != nil {
			return nil, err
		}
		var batch []ghRelease
		err = json.NewDecoder(resp.Body).Decode(&batch)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: GitHub answered %s", repo, resp.Status)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", repo, err)
		}
		for _, r := range batch {
			if !r.Draft && !r.Prerelease {
				out = append(out, r)
			}
		}
		if len(batch) < 100 {
			return out, nil
		}
	}
}

// download fetches an archive, refusing one larger than limit however its
// size was declared.
func (g *github) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("the archive is larger than %d MB", limit>>20)
	}
	return data, nil
}

// file reads a file of a repository as it is at ref, refusing one larger
// than limit.
func (g *github) file(ctx context.Context, repo, ref, name string, limit int64) ([]byte, error) {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	addr := fmt.Sprintf("%s/repos/%s/contents/%s?ref=%s", g.api, repo, strings.Join(parts, "/"), url.QueryEscape(ref))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.raw+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("%s has no %s at %s", repo, name, ref)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s: GitHub answered %s", repo, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d KB", name, limit>>10)
	}
	return data, nil
}
