package putingh

import (
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
		token: token,
		conf:  conf,
		cliv3: ghv3.NewClient(httpClient),
	}
}

type PutInGH struct {
	conf  Config
	token string
	cliv3 *ghv3.Client
}

func (s *PutInGH) PutInWithFile(ctx context.Context, uri, filename string) (string, error) {
	url, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	switch url.Scheme {
	case "git":
		sl := strings.Split(url.Path, "/")
		if len(sl) != 4 {
			return "", fmt.Errorf("%q not match git://owner/repo/branch/name", uri)
		}
		return s.PutInGitWithFile(ctx, url.Host, sl[1], sl[2], sl[3], filename)
	case "asset":
		sl := strings.Split(url.Path, "/")
		if len(sl) != 4 {
			return "", fmt.Errorf("%q not match asset://owner/repo/release/name", uri)
		}
		return s.PutInReleasesAssetWithFile(ctx, url.Host, sl[1], sl[2], sl[3], filename)
	case "gist":
		sl := strings.Split(url.Path, "/")
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
		sl := strings.Split(url.Path, "/")
		if len(sl) != 5 {
			return "", fmt.Errorf("%q not match git://owner/repo/branch/name", uri)
		}
		return s.PutInGit(ctx, url.Host, sl[1], sl[2], sl[3], r)
	case "asset":
		sl := strings.Split(url.Path, "/")
		if len(sl) != 5 {
			return "", fmt.Errorf("%q not match asset://owner/repo/release/name", uri)
		}
		return s.PutInReleasesAsset(ctx, url.Host, sl[1], sl[2], sl[3], r)
	case "gist":
		sl := strings.Split(url.Path, "/")
		if len(sl) != 4 {
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
	filename := filepath.Join(s.conf.TmpDir, owner, repo, release, name)
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

func (s *PutInGH) PutInGitWithFile(ctx context.Context, owner, repo, branch, name string, filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return s.PutInGit(ctx, owner, repo, branch, name, f)
}

func (s *PutInGH) PutInGit(ctx context.Context, owner, repo, branch, name string, r io.Reader) (string, error) {
	giturl := "https://github.com/" + owner + "/" + repo

	auth := &gogithttp.BasicAuth{
		Username: owner,
		Password: s.token,
	}

	dir := filepath.Join(s.conf.TmpDir, owner, repo, branch, name)
	os.MkdirAll(filepath.Dir(dir), 0755)

	remoteName := "origin-" + branch
	refName := plumbing.NewBranchReferenceName(branch)
	fetch := []gogitconfig.RefSpec{
		gogitconfig.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%[1]s", branch, remoteName)),
	}

	var resp *gogit.Repository
	_, err := os.Stat(dir + "/.git")
	if err == nil {
		resp, err = gogit.PlainOpen(dir)
	} else {
		resp, err = gogit.PlainInit(dir, false)
	}
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, dir)
	}
	err = resp.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, refName))
	if err != nil {
		return "", err
	}

	remote, err := resp.Remote(remoteName)
	if err != nil {
		if err != gogit.ErrRemoteNotFound {
			return "", err
		}
		c := &gogitconfig.RemoteConfig{
			Name:  remoteName,
			URLs:  []string{giturl},
			Fetch: fetch,
		}
		remote, err = resp.CreateRemote(c)
		if err != nil {
			return "", err
		}
	}

	_, err = resp.Branch(branch)
	if err != nil {
		if err != gogit.ErrBranchNotFound {
			return "", err
		}
		err = resp.CreateBranch(&gogitconfig.Branch{
			Name:   branch,
			Merge:  refName,
			Remote: remoteName,
		})
		if err != nil {
			return "", err
		}
		_, err = resp.Branch(branch)
		if err != nil {
			return "", err
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
			return "", fmt.Errorf("git fetch: %w", err)
		}
	}

	refIter, err := resp.Storer.IterReferences()
	if err != nil {
		return "", fmt.Errorf("iterReferences: %w", err)
	}
	ref, err := refIter.Next()
	if err != nil {
		return "", fmt.Errorf("next: %w", err)
	}
	if !ref.Hash().IsZero() {
		err = resp.Storer.SetReference(plumbing.NewHashReference(refName, ref.Hash()))
		if err != nil {
			return "", fmt.Errorf("setReference: %w", err)
		}

		work, err := resp.Worktree()
		if err != nil {
			return "", err
		}
		err = work.Reset(&gogit.ResetOptions{
			Commit: ref.Hash(),
			Mode:   gogit.HardReset,
		})
		if err != nil {
			return "", fmt.Errorf("git reset: %w", err)
		}
	}

	fname := filepath.Join(dir, name)
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

	work, err := resp.Worktree()
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
		err = resp.PushContext(ctx, &gogit.PushOptions{
			Auth:       auth,
			RemoteName: remoteName,
			Progress:   os.Stderr,
		})
		if err != nil {
			return "", fmt.Errorf("git push: %w", err)
		}
	}
	return giturl + "/raw/" + branch + "/" + name, nil
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
