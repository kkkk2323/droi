package main

import (
	"context"
	"encoding/json"

	"github.com/kkkk2323/droi/apps/native/pkg/droidpatch"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// download fetches the official build of version for this computer into
// path and checks it against Factory's checksum.
func download(ctx context.Context, s *droidpatch.Store, version, path string) error {
	return s.DownloadOfficial(ctx, version, path, nil)
}
