package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/wzshiming/putingh"
)

var usage = `putingh
Usage:
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
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 || len(args) > 2 {
		fmt.Fprint(os.Stderr, usage)
		return
	}
	token, ok := os.LookupEnv("GH_TOKEN")
	if !ok || token == "" {
		log.Fatal("GH_TOKEN can not be empty")
	}

	ctx := context.Background()
	if timeout := os.Getenv("TIMEOUT"); timeout != "" {
		d, err := time.ParseDuration(timeout)
		if err != nil {
			log.Printf("warning: parse error: TIMEOUT=%s: %s", timeout, err)
		} else {
			ctx, _ = context.WithTimeout(ctx, d)
		}
	}
	var options []putingh.Option
	if v, ok := os.LookupEnv("TMP_DIR"); ok {
		options = append(options, putingh.WithTmpDir(v))
	}
	putter := putingh.NewPutInGH(token, options...)

	if len(args) == 2 {
		url, err := putter.PutInWithFile(ctx, args[0], args[1])
		if err != nil {
			log.Fatalf("put error: %s", err)
		}
		fmt.Println(url)
	} else {
		r, err := putter.GetFrom(ctx, args[0])
		if err != nil {
			log.Fatalf("get error: %s", err)
		}
		io.Copy(os.Stdout, r)
	}
}
