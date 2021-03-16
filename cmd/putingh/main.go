package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/wzshiming/putingh"
)

var usage = `putingh
Usage:
	GH_TOKEN=xxx putingh git://owner/repo/branch/name localfile
	GH_TOKEN=xxx putingh asset://owner/repo/release/name localfile
	GH_TOKEN=xxx putingh gist://owner/description/name localfile
`

func main() {
	args := os.Args[1:]
	if len(args) != 2 {
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
	putter := putingh.NewPutInGH(token, putingh.Config{
		TmpDir:           os.Getenv("TMP_DIR"),
		GitName:          os.Getenv("GIT_NAME"),
		GitEmail:         os.Getenv("GIT_EMAIL"),
		GitCommitMessage: os.Getenv("GIT_COMMIT_MESSAGE"),
	})
	url, err := putter.PutInWithFile(ctx, args[0], args[1])
	if err != nil {
		log.Fatalf("put error: %s", err)
	}
	fmt.Println(url)
}
