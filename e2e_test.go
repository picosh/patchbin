package patchbin

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/picosh/patchbin/fixtures"
	"github.com/picosh/patchbin/util"
)

func TestE2E(t *testing.T) {
	testSingleTenantE2E(t)
	testMultiTenantE2E(t)
}

func testSingleTenantE2E(t *testing.T) {
	t.Log("single tenant end-to-end tests")
	dataDir := util.CreateTmpDir()
	defer func() {
		_ = os.RemoveAll(dataDir)
	}()
	suite := setupTest(dataDir, cfgSingleTenantTmpl)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := GitSshServer(ctx, suite.cfg)
	go func() {
		_ = s.ListenAndServe()
	}()
	// Hack to wait for startup
	time.Sleep(time.Millisecond * 100)

	// Users are auto-created on first use, no registration needed
	t.Log("User should be able to create a PR")
	suite.userKey.MustCmd(suite.patch, "pr create test")

	t.Log("Admin should also be able to create a PR")
	suite.adminKey.MustCmd(suite.patch, "pr create test")

	t.Log("List PRs")
	suite.userKey.MustCmd(nil, "pr ls")
}

func testMultiTenantE2E(t *testing.T) {
	t.Log("multi tenant end-to-end tests")
	dataDir := util.CreateTmpDir()
	defer func() {
		_ = os.RemoveAll(dataDir)
	}()
	suite := setupTest(dataDir, cfgMultiTenantTmpl)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := GitSshServer(ctx, suite.cfg)
	go func() {
		_ = s.ListenAndServe()
	}()

	time.Sleep(time.Millisecond * 100)

	// Users are auto-created on first use, no registration needed
	// In zero-trust model, no repo creation needed
	// Anyone can create PRs in any repo

	t.Log("User creates PR")
	output := suite.userKey.MustCmd(suite.patch, "pr create test")
	userPRID := util.ParsePRID(output)

	t.Log("User edits PR title (only creator can edit)")
	suite.userKey.MustCmd(nil, "pr edit "+userPRID+" Updated title")

	t.Log("User changes PR status to open (only creator can change status)")
	suite.userKey.MustCmd(nil, "pr open "+userPRID)

	t.Log("Admin creates PR")
	suite.adminKey.MustCmd(suite.patch, "pr create admin-repo")

	t.Log("Admin adds patchset to user's PR (zero-trust: anyone can add)")
	suite.adminKey.MustCmd(suite.otherPatch, "pr add "+userPRID)

	t.Log("User creates another PR and sets to open")
	output2 := suite.userKey.MustCmd(suite.patch, "pr create draft-repo")
	draftPRID := util.ParsePRID(output2)
	suite.userKey.MustCmd(nil, "pr open "+draftPRID)

	t.Log("List PRs")
	suite.userKey.MustCmd(nil, "pr ls")

	t.Log("View event logs")
	suite.userKey.MustCmd(nil, "logs")
}

type TestSuite struct {
	cfg        *GitCfg
	userKey    util.UserSSH
	adminKey   util.UserSSH
	patch      []byte
	otherPatch []byte
}

func setupTest(dataDir string, cfgTmpl string) TestSuite {
	opts := &slog.HandlerOptions{
		AddSource: true,
	}
	logger := slog.New(
		slog.NewTextHandler(os.Stdout, opts),
	)

	adminKey, userKey := util.GenerateKeys()
	cfgPath := util.CreateCfgFile(dataDir, cfgTmpl, adminKey)
	LoadConfigFile(cfgPath, logger)
	cfg := NewGitCfg(logger)

	// so outputs dont show dates
	cfg.TimeFormat = ""

	patch, err := fixtures.Fixtures.ReadFile("single.patch")
	if err != nil {
		panic(err)
	}
	otherPatch, err := fixtures.Fixtures.ReadFile("with-cover.patch")
	if err != nil {
		panic(err)
	}

	return TestSuite{cfg, userKey, adminKey, patch, otherPatch}
}

var cfgSingleTenantTmpl = `
url = "localhost"
data_dir = %q
admins = [%q]
time_format = "01/02/2006 15:04:05 07:00"`

var cfgMultiTenantTmpl = `
url = "localhost"
data_dir = %q
admins = [%q]
time_format = "01/02/2006 15:04:05 07:00"`
