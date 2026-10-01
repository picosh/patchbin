package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/picosh/patchbin"
	"github.com/picosh/patchbin/fixtures"
	"github.com/picosh/patchbin/util"
)

func main() {
	cleanupFlag := flag.Bool("cleanup", true, "Clean up tmp dir after quitting (default: true)")
	flag.Parse()

	opts := &slog.HandlerOptions{
		AddSource: true,
	}
	logger := slog.New(
		slog.NewTextHandler(os.Stdout, opts),
	)

	dataDir := util.CreateTmpDir()
	defer func() {
		if *cleanupFlag {
			_ = os.RemoveAll(dataDir)
		}
	}()

	adminKey, userKey := util.GenerateKeys()
	cfgPath := util.CreateCfgFile(dataDir, cfgTmpl, adminKey)
	patchbin.LoadConfigFile(cfgPath, logger)
	cfg := patchbin.NewGitCfg(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := patchbin.GitSshServer(ctx, cfg)
	go func() {
		_ = s.ListenAndServe()
	}()
	time.Sleep(time.Millisecond * 100)
	w := patchbin.GitWebServer(cfg)
	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.WebPort)
	go func() {
		_ = http.ListenAndServe(addr, w)
	}()

	// Hack to wait for startup
	time.Sleep(time.Millisecond * 100)

	patch, err := fixtures.Fixtures.ReadFile("single.patch")
	if err != nil {
		panic(err)
	}
	otherPatch, err := fixtures.Fixtures.ReadFile("with-cover.patch")
	if err != nil {
		panic(err)
	}
	rd1, err := fixtures.Fixtures.ReadFile("a_b_reorder.patch")
	if err != nil {
		panic(err)
	}
	rd2, err := fixtures.Fixtures.ReadFile("a_c_changed_commit.patch")
	if err != nil {
		panic(err)
	}

	// PR with title edited
	userKey.MustCmd(patch, "push test:simple-pr")
	userKey.MustCmd(nil, "edit test:simple-pr Simple PR")

	// PR with patchset added by another user
	userKey.MustCmd(patch, "push test:collab-pr")
	userKey.MustCmd(nil, "edit test:collab-pr Collaborative PR")
	adminKey.MustCmd(otherPatch, "push test:collab-pr")

	// Range Diff PR
	userKey.MustCmd(rd1, "push test:range-diff")
	userKey.MustCmd(nil, "edit test:range-diff Range Diff")
	userKey.MustCmd(rd2, "push test:range-diff")

	fmt.Println("time to do some testing...")
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
}

// args: tmpdir, adminKey
var cfgTmpl = `
url = "localhost"
data_dir = %q
admins = [%q]
time_format = "01/02/2006 15:04:05 07:00"`
