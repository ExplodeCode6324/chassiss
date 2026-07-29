package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

func readRemoteRef(
	ctx context.Context,
	runner gitstore.Runner,
	remote string,
	ref string,
) (string, bool, error) {
	result, err := runner.Run(ctx, "ls-remote", "--refs", remote, ref)
	if err != nil {
		return "", false, err
	}
	return parseRemoteRef(result.Stdout, ref)
}

func parseRemoteRef(data []byte, expectedRef string) (string, bool, error) {
	var oid string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != expectedRef {
			return "", false, fmt.Errorf("remote returned an unexpected ref record")
		}
		format := "sha1"
		if len(fields[0]) == 64 {
			format = "sha256"
		}
		if err := protocol.ValidateOID(fields[0], format); err != nil {
			return "", false, fmt.Errorf("remote returned an invalid object ID: %w", err)
		}
		if oid != "" && oid != fields[0] {
			return "", false, fmt.Errorf("remote returned conflicting values for %s", expectedRef)
		}
		oid = fields[0]
	}
	return oid, oid != "", nil
}
