package patchbin

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/ssh"
)

type Backend struct {
	Logger  *slog.Logger
	DB      *sqlx.DB
	Cfg     *GitCfg
	Limiter *RateLimiter
}

// Pubkey returns the standardized public key string for SSH.
func (be *Backend) Pubkey(pk ssh.PublicKey) string {
	return be.KeyForKeyText(pk)
}

func (be *Backend) KeyForFingerprint(pk ssh.PublicKey) string {
	return ssh.FingerprintSHA256(pk)
}

func (be *Backend) PubkeyToPublicKey(pubkey string) (ssh.PublicKey, error) {
	kk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubkey))
	return kk, err
}

func (be *Backend) KeyForKeyText(pk ssh.PublicKey) string {
	kb := base64.StdEncoding.EncodeToString(pk.Marshal())
	return fmt.Sprintf("%s %s", pk.Type(), kb)
}

func (be *Backend) KeysEqual(pka, pkb string) bool {
	return pka == pkb
}

func (be *Backend) PublicKeysEqual(a, b ssh.PublicKey) bool {
	return string(a.Marshal()) == string(b.Marshal())
}

func (be *Backend) IsAdmin(pk ssh.PublicKey) bool {
	for _, apk := range be.Cfg.Admins {
		if be.PublicKeysEqual(pk, apk) {
			return true
		}
	}
	return false
}

// ComputeUserName derives a username from an SSH public key.
// Uses the first 8 characters of the SHA256 hash of the key.
func (be *Backend) ComputeUserName(pubkey string) string {
	hash := sha256.Sum256([]byte(pubkey))
	return hex.EncodeToString(hash[:4])
}
