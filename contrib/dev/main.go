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

	// Opened patch (creator opens their own PR)
	userKey.MustCmd(patch, "pr create test")
	userKey.MustCmd(nil, "pr edit 1 Opened patch")
	userKey.MustCmd(nil, `pr open --comment "ready for review" 1`)

	// Drafted patch (creator sets back to draft)
	userKey.MustCmd(patch, "pr create test")
	userKey.MustCmd(nil, "pr edit 2 Drafted patch")
	userKey.MustCmd(nil, `pr draft --comment "need more work" 2`)

	// Opened then re-drafted by creator
	userKey.MustCmd(patch, "pr create test")
	userKey.MustCmd(nil, "pr edit 3 Opened then re-drafted")
	userKey.MustCmd(nil, `pr open 3`)
	userKey.MustCmd(nil, `pr draft --comment "Woops, didn't mean to submit yet" 3`)

	// Patchset added by another user, creator opens
	userKey.MustCmd(patch, "pr create test")
	userKey.MustCmd(nil, "pr edit 5 Patchset from another user")
	adminKey.MustCmd(otherPatch, `pr add 5`)
	userKey.MustCmd(nil, `pr open --comment "updated with feedback" 5`)

	// Patchset added by another user, creator drafts
	userKey.MustCmd(patch, "pr create test")
	userKey.MustCmd(nil, "pr edit 6 Patchset then drafted")
	adminKey.MustCmd(otherPatch, `pr add 6`)
	userKey.MustCmd(nil, `pr draft --comment "taking a step back on this" 6`)

	// Range Diff
	userKey.MustCmd(rd1, "pr create test")
	userKey.MustCmd(nil, "pr edit 7 Range Diff")
	userKey.MustCmd(rd2, "pr add 7")

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
