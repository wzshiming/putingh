package putingh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	ghv3 "github.com/google/go-github/v33/github"
	"golang.org/x/oauth2"
)

var (
	ErrNotFound = fmt.Errorf("not found")
)

type Config struct {
	TmpDir           string
	GitName          string
	GitEmail         string
	GitCommitMessage string
}

func (c *Config) setDefault() {
	if c.TmpDir == "" {
		c.TmpDir = "./tmp/"
	}
	if c.GitName == "" {
		c.GitName = "bot"
	}
}

func NewPutInGH(token string, conf Config) *PutInGH {
	conf.setDefault()
	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	httpClient := oauth2.NewClient(context.Background(), src)
	return &PutInGH{
		token:   token,
		conf:    conf,
		httpCli: httpClient,
		cliv3:   ghv3.NewClient(httpClient),
	}
}

type PutInGH struct {
	conf    Config
	token   string
	httpCli *http.Client
	cliv3   *ghv3.Client
}

func (s *PutInGH) GetFrom(ctx context.Context, uri string) (io.Reader, error) {
	url, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	switch url.Scheme {
	case "git":
		sl := strings.SplitN(url.Path, "/", 4)
		if len(sl) != 4 {
			return nil, fmt.Errorf("%q not match git://owner/repo/branch/name", uri)
		}
		return s.GetFromGit(ctx, url.Host, sl[1], sl[2], sl[3])
	case "asset":
		sl := strings.SplitN(url.Path, "/", 4)
		if len(sl) != 4 {
			return nil, fmt.Errorf("%q not match asset://owner/repo/release/name", uri)
		}
		return s.GetFromReleasesAsset(ctx, url.Host, sl[1], sl[2], sl[3])
	case "gist":
		sl := strings.SplitN(url.Path, "/", 3)
		if len(sl) != 3 {
			return nil, fmt.Errorf("%q not match gist://owner/description/name", uri)
		}
		return s.GetFromGist(ctx, url.Host, sl[1], sl[2])
	}
	return nil, fmt.Errorf("%q not support", uri)
}

func (s *PutInGH) PutInWithFile(ctx context.Context, uri, filename string) (string, error) {
	url, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	switch url.Scheme {
	case "git":
		sl := strings.SplitN(url.Path, "/", 4)
		if len(sl) != 4 {
			return "", fmt.Errorf("%q not match git://owner/repo/branch/name", uri)
		}
		return s.PutInGitWithFile(ctx, url.Host, sl[1], sl[2], sl[3], filename)
	case "asset":
		sl := strings.SplitN(url.Path, "/", 4)
		if len(sl) != 4 {
			return "", fmt.Errorf("%q not match asset://owner/repo/release/name", uri)
		}
		return s.PutInReleasesAssetWithFile(ctx, url.Host, sl[1], sl[2], sl[3], filename)
	case "gist":
		sl := strings.SplitN(url.Path, "/", 3)
		if len(sl) != 3 {
			return "", fmt.Errorf("%q not match gist://owner/description/name", uri)
		}
		return s.PutInGistWithFile(ctx, url.Host, sl[1], sl[2], filename)
	}
	return "", fmt.Errorf("%q not support", uri)
}

func (s *PutInGH) PutIn(ctx context.Context, uri string, r io.Reader) (string, error) {
	url, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	switch url.Scheme {
	case "git":
		sl := strings.SplitN(url.Path, "/", 4)
		if len(sl) != 4 {
			return "", fmt.Errorf("%q not match git://owner/repo/branch/name", uri)
		}
		return s.PutInGit(ctx, url.Host, sl[1], sl[2], sl[3], r)
	case "asset":
		sl := strings.SplitN(url.Path, "/", 4)
		if len(sl) != 4 {
			return "", fmt.Errorf("%q not match asset://owner/repo/release/name", uri)
		}
		return s.PutInReleasesAsset(ctx, url.Host, sl[1], sl[2], sl[3], r)
	case "gist":
		sl := strings.SplitN(url.Path, "/", 3)
		if len(sl) != 3 {
			return "", fmt.Errorf("%q not match gist://owner/description/name", uri)
		}
		return s.PutInGist(ctx, url.Host, sl[1], sl[2], r)
	}
	return "", fmt.Errorf("%q not support", uri)
}

func (s *PutInGH) PutInGistWithFile(ctx context.Context, owner, description, name string, filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return s.PutInGist(ctx, owner, description, name, f)
}

func (s *PutInGH) GetFromGist(ctx context.Context, owner, description, name string) (io.Reader, error) {
	var oriGist *ghv3.Gist
	err := s.eachGist(ctx, owner, func(gists []*ghv3.Gist) bool {
		for _, gist := range gists {
			if gist.Description != nil && *gist.Description == description {
				oriGist = gist
				return false
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if oriGist == nil {
		return nil, ErrNotFound
	}
	file, ok := oriGist.Files[ghv3.GistFilename(name)]
	if !ok {
		return nil, ErrNotFound
	}

	if file.Content != nil {
		return bytes.NewBufferString(*file.Content), nil
	}

	if file.RawURL != nil {
		resp, err := s.httpGet(ctx, *file.RawURL)
		if err != nil {
			return nil, err
		}
		return newReaderWithAutoCloser(resp.Body), nil
	}
	return nil, ErrNotFound
}

func (s *PutInGH) PutInGist(ctx context.Context, owner, description, name string, r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	dataContext := string(data)

	var oriGist *ghv3.Gist
	err = s.eachGist(ctx, owner, func(gists []*ghv3.Gist) bool {
		for _, gist := range gists {
			if gist.Description != nil && *gist.Description == description {
				oriGist = gist
				return false
			}
		}
		return true
	})
	if err != nil {
		return "", err
	}

	var raw string
	if oriGist == nil {
		gist, _, err := s.cliv3.Gists.Create(ctx, &ghv3.Gist{
			Public: ghv3.Bool(true),
			Files: map[ghv3.GistFilename]ghv3.GistFile{
				ghv3.GistFilename(name): {
					Content: &dataContext,
				},
			},
			Description: &description,
		})
		if err != nil {
			return "", err
		}
		raw = *gist.Files[ghv3.GistFilename(name)].RawURL
	} else {
		file := oriGist.Files[ghv3.GistFilename(name)]
		if file.Content != nil && *file.Content == dataContext {
			raw = *oriGist.Files[ghv3.GistFilename(name)].RawURL
		} else {
			oriGist.Files[ghv3.GistFilename(name)] = ghv3.GistFile{
				Filename: &name,
				Content:  &dataContext,
			}
			gist, _, err := s.cliv3.Gists.Edit(ctx, *oriGist.ID, oriGist)
			if err != nil {
				return "", err
			}
			raw = *gist.Files[ghv3.GistFilename(name)].RawURL
		}
	}
	raw = strings.SplitN(raw, "/raw/", 2)[0] + "/raw/" + name
	return raw, nil
}

func (s *PutInGH) GetFromReleasesAsset(ctx context.Context, owner, repo, release, name string) (io.Reader, error) {
	var releaseID *int64
	err := s.eachReleases(ctx, owner, repo, func(releases []*ghv3.RepositoryRelease) bool {
		for _, r := range releases {
			if r.Name != nil && *r.Name == release {
				releaseID = r.ID
				return false
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	if releaseID == nil {
		return nil, ErrNotFound
	}
	repositoryRelease, _, err := s.cliv3.Repositories.GetRelease(ctx, owner, repo, *releaseID)
	if err != nil {
		return nil, err
	}

	downloadURL := ""
	for _, asset := range repositoryRelease.Assets {
		if *asset.Name == name {
			if asset.BrowserDownloadURL == nil {
				return nil, ErrNotFound
			}
			downloadURL = *asset.BrowserDownloadURL

		}
	}
	if downloadURL == "" {
		return nil, ErrNotFound
	}

	resp, err := s.httpGet(ctx, downloadURL)
	if err != nil {
		return nil, err
	}
	return newReaderWithAutoCloser(resp.Body), nil

}

func (s *PutInGH) PutInReleasesAssetWithFile(ctx context.Context, owner, repo, release, name string, filename string) (string, error) {
	var releaseID *int64
	err := s.eachReleases(ctx, owner, repo, func(releases []*ghv3.RepositoryRelease) bool {
		for _, r := range releases {
			if r.Name != nil && *r.Name == release {
				releaseID = r.ID
				return false
			}
		}
		return true
	})
	if err != nil {
		return "", err
	}

	if releaseID == nil {
		repositoryRelease, _, err := s.cliv3.Repositories.CreateRelease(ctx, owner, repo, &ghv3.RepositoryRelease{
			Name:    &release,
			TagName: &release,
			Draft:   new(bool),
		})
		if err != nil {
			return "", err
		}
		releaseID = repositoryRelease.ID
	} else {
		repositoryRelease, _, err := s.cliv3.Repositories.GetRelease(ctx, owner, repo, *releaseID)
		if err != nil {
			return "", err
		}

		for _, asset := range repositoryRelease.Assets {
			if *asset.Name == name {
				_, err := s.cliv3.Repositories.DeleteReleaseAsset(ctx, owner, repo, *asset.ID)
				if err != nil {
					return "", err
				}
				break
			}
		}
	}

	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()

	respAsset, _, err := s.cliv3.Repositories.UploadReleaseAsset(ctx, owner, repo, *releaseID, &ghv3.UploadOptions{
		Name: name,
	}, f)
	if err != nil {
		return "", err
	}
	return *respAsset.BrowserDownloadURL, nil
}

func (s *PutInGH) PutInReleasesAsset(ctx context.Context, owner, repo, release, name string, r io.Reader) (string, error) {
	filename := filepath.Join(s.conf.TmpDir, "asset", owner, repo, release, name)
	os.MkdirAll(filepath.Dir(filename), 0755)
	f, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, r)
	if err != nil {
		return "", err
	}
	f.Close()
	return s.PutInReleasesAssetWithFile(ctx, owner, repo, release, name, filename)
}

func (s *PutInGH) GetFromGit(ctx context.Context, owner, repo, branch, name string) (io.Reader, error) {
	dir, _, err := s.fetchGit(ctx, owner, repo, branch, name)
	if err != nil {
		return nil, err
	}
	fname := filepath.Join(dir, name)
	f, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	return newReaderWithAutoCloser(f), nil
}

func (s *PutInGH) PutInGitWithFile(ctx context.Context, owner, repo, branch, name string, filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return s.PutInGit(ctx, owner, repo, branch, name, f)
}

func (s *PutInGH) PutInGit(ctx context.Context, owner, repo, branch, name string, r io.Reader) (string, error) {
	dir, repository, err := s.fetchGit(ctx, owner, repo, branch, name)
	if err != nil {
		return "", err
	}
	fname := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(fname), 0755)
	f, err := os.OpenFile(fname, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, r)
	if err != nil {
		f.Close()
		return "", err
	}
	f.Close()

	work, err := repository.Worktree()
	if err != nil {
		return "", err
	}
	_, err = work.Add(name)
	if err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}
	status, err := work.Status()
	if err != nil {
		return "", err
	}

	if len(status) != 0 &&
		status[name] != nil &&
		(status[name].Staging != gogit.Unmodified || status[name].Worktree != gogit.Unmodified) {
		now := time.Now()

		message := s.conf.GitCommitMessage
		if message == "" {
			message = fmt.Sprintf("Automatic updated %s", now.Format(time.RFC3339))
		}
		_, err = work.Commit(message, &gogit.CommitOptions{
			Author: &object.Signature{
				Name:  s.conf.GitName,
				Email: s.conf.GitEmail,
				When:  now,
			},
		})
		if err != nil {
			return "", fmt.Errorf("git commit: %w", err)
		}
		err = repository.PushContext(ctx, &gogit.PushOptions{
			Auth:       s.gitBasicAuth(owner),
			RemoteName: s.gitRemoteName(branch),
			Progress:   os.Stderr,
		})
		if err != nil {
			return "", fmt.Errorf("git push: %w", err)
		}
	}
	return s.gitURL(owner, repo) + "/raw/" + branch + "/" + name, nil
}

func (s *PutInGH) fetchGit(ctx context.Context, owner, repo, branch, name string) (string, *gogit.Repository, error) {
	giturl := s.gitURL(owner, repo)

	auth := s.gitBasicAuth(owner)

	dir := filepath.Join(s.conf.TmpDir, "git", owner, repo, branch)
	os.MkdirAll(filepath.Dir(dir), 0755)

	remoteName := s.gitRemoteName(branch)
	refName := plumbing.NewBranchReferenceName(branch)
	fetch := []gogitconfig.RefSpec{
		gogitconfig.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%[1]s", branch, remoteName)),
	}

	var repository *gogit.Repository
	_, err := os.Stat(dir + "/.git")
	if err == nil {
		repository, err = gogit.PlainOpen(dir)
	} else {
		repository, err = gogit.PlainInit(dir, false)
	}
	if err != nil {
		return "", nil, fmt.Errorf("%w: %s", err, dir)
	}

	err = repository.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, refName))
	if err != nil {
		return "", nil, err
	}

	remote, err := repository.Remote(remoteName)
	if err != nil {
		if err != gogit.ErrRemoteNotFound {
			return "", nil, err
		}
		c := &gogitconfig.RemoteConfig{
			Name:  remoteName,
			URLs:  []string{giturl},
			Fetch: fetch,
		}
		remote, err = repository.CreateRemote(c)
		if err != nil {
			return "", nil, err
		}
	}

	_, err = repository.Branch(branch)
	if err != nil {
		if err != gogit.ErrBranchNotFound {
			return "", nil, err
		}
		err = repository.CreateBranch(&gogitconfig.Branch{
			Name:   branch,
			Merge:  refName,
			Remote: remoteName,
		})
		if err != nil {
			return "", nil, err
		}
		_, err = repository.Branch(branch)
		if err != nil {
			return "", nil, err
		}
	}

	err = remote.FetchContext(ctx, &gogit.FetchOptions{
		RemoteName: remoteName,
		RefSpecs:   fetch,
		Progress:   os.Stderr,
		Auth:       auth,
	})
	if err != nil && err != gogit.NoErrAlreadyUpToDate {
		if _, ok := err.(gogit.NoMatchingRefSpecError); !ok {
			return "", nil, fmt.Errorf("git fetch: %w", err)
		}
	}

	refIter, err := repository.Storer.IterReferences()
	if err != nil {
		return "", nil, fmt.Errorf("iterReferences: %w", err)
	}
	ref, err := refIter.Next()
	if err != nil {
		return "", nil, fmt.Errorf("next: %w", err)
	}
	if !ref.Hash().IsZero() {
		err = repository.Storer.SetReference(plumbing.NewHashReference(refName, ref.Hash()))
		if err != nil {
			return "", nil, fmt.Errorf("setReference: %w", err)
		}

		work, err := repository.Worktree()
		if err != nil {
			return "", nil, err
		}
		err = work.Reset(&gogit.ResetOptions{
			Commit: ref.Hash(),
			Mode:   gogit.HardReset,
		})
		if err != nil {
			return "", nil, fmt.Errorf("git reset: %w", err)
		}
	}

	return dir, repository, nil
}

func (s *PutInGH) gitRemoteName(branch string) string {
	return "origin-" + branch
}

func (s *PutInGH) gitBasicAuth(owner string) *gogithttp.BasicAuth {
	return &gogithttp.BasicAuth{
		Username: owner,
		Password: s.token,
	}
}

func (s *PutInGH) gitURL(owner, repo string) string {
	return "https://github.com/" + owner + "/" + repo
}

func (s *PutInGH) httpGet(ctx context.Context, uri string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	return s.httpCli.Do(req)
}

func (s *PutInGH) eachReleases(ctx context.Context, owner, repo string, next func([]*ghv3.RepositoryRelease) bool) error {
	opt := &ghv3.ListOptions{
		PerPage: 100,
	}

	for {
		list, resp, err := s.cliv3.Repositories.ListReleases(ctx, owner, repo, opt)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotFound {
				return nil
			}
			return err
		}
		if next != nil && !next(list) {
			break
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return nil
}

func (s *PutInGH) eachGist(ctx context.Context, owner string, next func([]*ghv3.Gist) bool) error {
	opt := ghv3.ListOptions{
		PerPage: 100,
	}
	for {
		list, resp, err := s.cliv3.Gists.List(ctx, owner, &ghv3.GistListOptions{
			ListOptions: opt,
		})
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotFound {
				return nil
			}
			return err
		}
		if next != nil && !next(list) {
			break
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return nil
}
