package patchbin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/picosh/pico/pkg/pssh"
	"golang.org/x/crypto/ssh"
)

func authHandler(pr *PrCmd) func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	return func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		pubkey := pr.Backend.Pubkey(key)
		userName := conn.User()
		perms := &ssh.Permissions{
			Extensions: map[string]string{
				"pubkey": pubkey,
			},
		}
		err := pr.IsBanned(pubkey, userName)
		if err != nil {
			pr.Backend.Logger.Info(
				"user denied access",
				"err", err,
				"username", userName,
				"pubkey", pubkey,
			)
			return perms, err
		}
		return perms, nil
	}
}

func GitSshServer(ctx context.Context, cfg *GitCfg) *pssh.SSHServer {
	dbpath := filepath.Join(cfg.DataDir, "pr.db?_fk=on")
	dbh, err := SqliteOpen("file:"+dbpath, cfg.Logger)
	if err != nil {
		panic(fmt.Sprintf("cannot find database file, check folder and perms: %s: %s", dbpath, err))
	}

	interval, err := time.ParseDuration(cfg.RateLimitInterval)
	if err != nil {
		panic(fmt.Sprintf("invalid rate_limit_interval: %s: %s", cfg.RateLimitInterval, err))
	}

	be := &Backend{
		DB:      dbh,
		Logger:  cfg.Logger,
		Cfg:     cfg,
		Limiter: NewRateLimiter(cfg.RateLimitCount, interval),
	}

	prCmd := &PrCmd{
		Backend: be,
	}

	server, err := pssh.NewSSHServerWithConfig(
		ctx,
		cfg.Logger,
		"patchbin",
		cfg.Host,
		cfg.SshPort,
		cfg.PromPort,
		filepath.Join(cfg.DataDir, "term_info_ed25519"),
		authHandler(prCmd),
		[]pssh.SSHServerMiddleware{
			GitPatchRequestMiddleware(be, prCmd),
		},
		[]pssh.SSHServerMiddleware{},
		nil,
	)

	if err != nil {
		cfg.Logger.Error("failed to create ssh server", "err", err)
		os.Exit(1)
	}

	return server
}
