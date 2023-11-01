package putingh

import (
	"bytes"
	"context"
	"errors"
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
	"github.com/go-git/go-git/v5/plumbing/transport"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	ghv3 "github.com/google/go-github/v56/github"
	"golang.org/x/oauth2"
)

var (
	DefaultOptions = []Option{
		WithHost("https://github.com"),
		WithGitAuthorSignature("bot", ""),
		WithTmpDir("./tmp/"),
		WithOutput(io.Discard),
		WithPerPage(100),
		WithContext(context.Background()),
		WithGitCommitMessage(func(owner, repo, branch, name, path string) string {
			return fmt.Sprintf("Automatic update %s", name)
		}),
	}

	ErrNotFound = fmt.Errorf("not found")

	anyFile = "*"
)

type Option func(p *PutInGH)

func NewPutInGH(token string, options ...Option) *PutInGH {
	p := &PutInGH{
		token: token,
	}

	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	httpClient := oauth2.NewClient(p.ctx, src)
	p.httpCli = httpClient

	for _, opt := range DefaultOptions {
		if opt != nil {
			opt(p)
		}
	}
	for _, opt := range options {
		if opt != nil {
			opt(p)
		}
	}
	p.cliv3 = ghv3.NewClient(httpClient)
	return p
}

func WithTmpDir(dir string) Option {
	return func(p *PutInGH) {
		p.tmpDir = dir
	}
}

func WithGitCommitMessage(fn func(owner, repo, branch, name, path string) string) Option {
	return func(p *PutInGH) {
		p.gitCommitMessage = fn
	}
}

func WithGitAuthorSignature(username, email string) Option {
	return WithGitCommitOptions(func(owner, repo, branch, name, path string) *gogit.CommitOptions {
		return &gogit.CommitOptions{
			Author: &object.Signature{
				Name:  username,
				Email: email,
				When:  time.Now(),
			},
		}
	})
}

func WithGitCommitOptions(fn func(owner, repo, branch, name, path string) (opt *gogit.CommitOptions)) Option {
	return func(p *PutInGH) {
		p.gitCommitOption = fn
	}
}

func WithContext(ctx context.Context) Option {
	return func(p *PutInGH) {
		p.ctx = ctx
	}
}

func WithOutput(out io.Writer) Option {
	return func(p *PutInGH) {
		p.out = out
	}
}

func WithHost(host string) Option {
	return func(p *PutInGH) {
		p.host = host
	}
}

func WithPerPage(perPage int) Option {
	return func(p *PutInGH) {
		p.perPage = perPage
	}
}

func WithHTTPClient(fun func(cli *http.Client) *http.Client) Option {
	return func(p *PutInGH) {
		p.httpCli = fun(p.httpCli)
	}
}

type PutInGH struct {
	tmpDir           string
	gitCommitMessage func(owner, repo, branch, name, path string) (msg string)
	gitCommitOption  func(owner, repo, branch, name, path string) (opt *gogit.CommitOptions)
	ctx              context.Context
	out              io.Writer
	host             string
	perPage          int

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
			return nil, fmt.Errorf("%q not match git://owner/repository/branch/name", uri)
		}
		return s.GetFromGit(ctx, url.Host, sl[1], sl[2], sl[3])
	case "asset":
		sl := strings.SplitN(url.Path, "/", 4)
		if len(sl) != 4 {
			return nil, fmt.Errorf("%q not match asset://owner/repository/release/name", uri)
		}
		return s.GetFromReleasesAsset(ctx, url.Host, sl[1], sl[2], sl[3])
	case "gist":
		sl := strings.SplitN(url.Path, "/", 3)
		if len(sl) != 3 {
			return nil, fmt.Errorf("%q not match gist://owner/gist_id/name", uri)
		}
		return s.GetFromGist(ctx, url.Host, sl[1], sl[2])
	}
	return nil, fmt.Errorf("%q not support", uri)
}

func (s *PutInGH) PutInWithFile(ctx context.Context, uri, filename string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "git":
		sl := strings.SplitN(u.Path, "/", 4)
		if len(sl) != 4 {
			return "", fmt.Errorf("%q not match git://owner/repository/branch/name", uri)
		}
		return s.putInGitWithFile(ctx, u.Host, sl[1], sl[2], sl[3], filename)
	case "asset":
		sl := strings.SplitN(u.Path, "/", 4)
		if len(sl) != 4 {
			return "", fmt.Errorf("%q not match asset://owner/repository/release/name", uri)
		}
		return s.putInReleasesAssetWithFile(ctx, u.Host, sl[1], sl[2], sl[3], filename)
	case "gist":
		sl := strings.SplitN(u.Path, "/", 3)
		if len(sl) != 3 {
			return "", fmt.Errorf("%q not match gist://owner/gist_id/name", uri)
		}
		return s.putInGistWithFile(ctx, u.Host, sl[1], sl[2], filename)
	}
	return "", fmt.Errorf("%q not support", uri)
}

func (s *PutInGH) PutIn(ctx context.Context, uri string, r io.Reader) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "git":
		sl := strings.SplitN(u.Path, "/", 4)
		if len(sl) != 4 {
			return "", fmt.Errorf("%q not match git://owner/repository/branch/name", uri)
		}
		return s.putInGit(ctx, u.Host, sl[1], sl[2], sl[3], r)
	case "asset":
		sl := strings.SplitN(u.Path, "/", 4)
		if len(sl) != 4 {
			return "", fmt.Errorf("%q not match asset://owner/repository/release/name", uri)
		}
		return s.putInReleasesAsset(ctx, u.Host, sl[1], sl[2], sl[3], r)
	case "gist":
		sl := strings.SplitN(u.Path, "/", 3)
		if len(sl) != 3 {
			return "", fmt.Errorf("%q not match gist://owner/gist_id/name", uri)
		}
		return s.putInGist(ctx, u.Host, sl[1], sl[2], r)
	}
	return "", fmt.Errorf("%q not support", uri)
}

func (s *PutInGH) putInGistWithFile(ctx context.Context, owner, gistId, name string, filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return s.putInGist(ctx, owner, gistId, name, f)
}

func (s *PutInGH) GetFromGist(ctx context.Context, owner, gistId, name string) (io.Reader, error) {
	var oriGist *ghv3.Gist
	err := s.eachGist(ctx, owner, func(gists []*ghv3.Gist) bool {
		for _, gist := range gists {
			if gistId == anyFile {
				_, ok := gist.Files[ghv3.GistFilename(name)]
				if ok {
					oriGist = gist
					return false
				}
			} else if *gist.ID == gistId {
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

func (s *PutInGH) putInGist(ctx context.Context, owner, gistId, name string, r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	dataContext := string(data)

	var oriGist *ghv3.Gist
	err = s.eachGist(ctx, owner, func(gists []*ghv3.Gist) bool {
		for _, gist := range gists {
			if gistId == anyFile {
				_, ok := gist.Files[ghv3.GistFilename(name)]
				if ok {
					oriGist = gist
					return false
				}
			} else if *gist.ID == gistId {
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
			Description: &gistId,
		})
		if err != nil {
			return "", err
		}
		raw = *gist.Files[ghv3.GistFilename(name)].RawURL
	} else {
		oriGist.Files = map[ghv3.GistFilename]ghv3.GistFile{
			ghv3.GistFilename(name): {
				Filename: &name,
				Content:  &dataContext,
			},
		}
		gist, _, err := s.cliv3.Gists.Edit(ctx, *oriGist.ID, oriGist)
		if err != nil {
			return "", err
		}
		raw = *gist.Files[ghv3.GistFilename(name)].RawURL
	}
	raw = strings.SplitN(raw, "/raw/", 2)[0] + "/raw/" + name
	return raw, nil
}

func (s *PutInGH) GetFromReleasesAsset(ctx context.Context, owner, repo, release, name string) (io.Reader, error) {
	respRelease, response, err := s.cliv3.Repositories.GetReleaseByTag(ctx, owner, repo, release)
	if err != nil && response.StatusCode != http.StatusNotFound {
		return nil, err
	}

	var releaseID *int64
	if respRelease != nil {
		releaseID = respRelease.ID
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

func (s *PutInGH) putInReleasesAssetWithFile(ctx context.Context, owner, repo, release, name string, filename string) (string, error) {
	respRelease, response, err := s.cliv3.Repositories.GetReleaseByTag(ctx, owner, repo, release)
	if err != nil && response.StatusCode != http.StatusNotFound {
		return "", err
	}

	var releaseID *int64
	if respRelease != nil {
		releaseID = respRelease.ID
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

func (s *PutInGH) putInReleasesAsset(ctx context.Context, owner, repo, release, name string, r io.Reader) (string, error) {
	filename := filepath.Join(s.tmpDir, "asset", owner, repo, release, name)
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
	return s.putInReleasesAssetWithFile(ctx, owner, repo, release, name, filename)
}

func (s *PutInGH) GetFromGit(ctx context.Context, owner, repo, branch, name string) (io.Reader, error) {
	dir, _, err := s.fetchGit(ctx, owner, repo, branch)
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

func (s *PutInGH) putInGitWithFile(ctx context.Context, owner, repo, branch, name string, filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return s.putInGit(ctx, owner, repo, branch, name, f)
}

func (s *PutInGH) putInGit(ctx context.Context, owner, repo, branch, name string, r io.Reader) (string, error) {
	dir, repository, err := s.fetchGit(ctx, owner, repo, branch)
	if err != nil {
		return "", err
	}
	fname := filepath.Join(dir, name)
	err = os.MkdirAll(filepath.Dir(fname), 0755)
	if err != nil {
		return "", err
	}
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
		opt := s.gitCommitOption(owner, repo, branch, name, fname)
		message := s.gitCommitMessage(owner, repo, branch, name, fname)
		_, err = work.Commit(message, opt)
		if err != nil {
			return "", fmt.Errorf("git commit: %w", err)
		}
		err = repository.PushContext(ctx, &gogit.PushOptions{
			Auth:       s.gitBasicAuth(owner),
			RemoteName: s.gitRemoteName(branch),
			Progress:   s.out,
		})
		if err != nil {
			return "", fmt.Errorf("git push: %w", err)
		}
	}
	return s.gitURL(owner, repo) + "/raw/" + branch + "/" + name, nil
}

func (s *PutInGH) fetchGit(ctx context.Context, owner, repo, branch string) (string, *gogit.Repository, error) {
	giturl := s.gitURL(owner, repo)

	auth := s.gitBasicAuth(owner)

	dir := filepath.Join(s.tmpDir, "git", owner, repo, branch)
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
		if !errors.Is(err, gogit.ErrRemoteNotFound) {
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
		if !errors.Is(err, gogit.ErrBranchNotFound) {
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
		Progress:   s.out,
		Auth:       auth,
	})
	if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) && !errors.Is(err, transport.ErrEmptyRemoteRepository) {
		var noMatchingRefSpecError gogit.NoMatchingRefSpecError
		if !errors.As(err, &noMatchingRefSpecError) {
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
	return strings.Join([]string{s.host, owner, repo}, "/")
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
		PerPage: s.perPage,
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
		PerPage: s.perPage,
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
