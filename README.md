# putingh (Put in GH)
Put file in the GH

[![Go Report Card](https://goreportcard.com/badge/github.com/wzshiming/putingh)](https://goreportcard.com/report/github.com/wzshiming/putingh)
[![GoDoc](https://pkg.go.dev/badge/github.com/wzshiming/putingh)](https://pkg.go.dev/github.com/wzshiming/putingh)
[![GitHub license](https://img.shields.io/github/license/wzshiming/putingh.svg)](https://github.com/wzshiming/putingh/blob/master/LICENSE)

## Usage

``` bash
# Put file in git repository
GH_TOKEN=you_github_token putingh git://owner/repository/branch/name[/name]... localfile

# Put file in git repository release assets
GH_TOKEN=you_github_token putingh asset://owner/repository/release/name localfile

# Put file in gist
GH_TOKEN=you_github_token putingh gist://owner/gist_id/name localfile

# Get file from git repository
GH_TOKEN=you_github_token putingh git://owner/repository/branch/name[/name]...

# Get file from git repository release assets
GH_TOKEN=you_github_token putingh asset://owner/repository/release/name

# Get file from gist
GH_TOKEN=you_github_token putingh gist://owner/gist_id/name
```

## Example

[wzshiming/action-upload-release-assets](https://github.com/wzshiming/action-upload-release-assets)

## License

Licensed under the MIT License. See [LICENSE](https://github.com/wzshiming/putingh/blob/master/LICENSE) for the full license text.
