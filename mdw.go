package patchbin

import (
	"fmt"
	"strings"

	"github.com/picosh/pico/pkg/pssh"
)

func GitPatchRequestMiddleware(be *Backend, pr GitPatchRequest) pssh.SSHServerMiddleware {
	return func(next pssh.SSHServerHandler) pssh.SSHServerHandler {
		return func(sesh *pssh.SSHServerConnSession) error {
			args := sesh.Command()
			if len(args) > 0 {
				first := args[0]
				if strings.Contains(first, ":") {
					if strings.HasSuffix(first, ".patch") {
						args = append([]string{"pull", strings.TrimSuffix(first, ".patch")}, args[1:]...)
					} else {
						args = append([]string{"push", first}, args[1:]...)
					}
				}
			}
			cli := NewCli(sesh, be, pr)
			margs := append([]string{"git"}, args...)
			be.Logger.Info("ssh args", "args", args)
			err := cli.Run(margs)
			if err != nil {
				be.Logger.Error("error when running cli", "err", err)
				sesh.Fatal(fmt.Errorf("err: %w", err))
				_ = next(sesh)
				return err
			}

			return nil
		}
	}
}
