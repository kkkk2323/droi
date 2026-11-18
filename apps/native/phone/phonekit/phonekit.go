// Package phonekit is what the Phone App asks of iOS that MyGo has no
// plugin for, in Swift beside it: scanning the pairing code, photos picked
// and sent as JPEG, and the alert sounds. Elsewhere its calls fail with
// MyGo's errors.ErrUnsupported.
package phonekit

import (
	"context"

	"github.com/egoist/mygo"
)

const pluginID = "droiphonekit"

// ScanQR shows the camera until it reads a QR code, and returns its text:
// "" when the user cancelled. hint shows over the camera, cancel on its
// button. Call it off the main goroutine, as it waits for the user.
func ScanQR(ctx context.Context, hint, cancel string) (string, error) {
	var r struct {
		Code string `json:"code"`
	}
	err := mygo.Native(pluginID).Call(ctx, "scan", map[string]any{"hint": hint, "cancel": cancel}, &r)
	return r.Code, err
}

// PickImages shows the system's photo picker and returns the paths of JPEG
// copies of the photos chosen, their longest side at most 1568 pixels: none
// when the user went back. Call it off the main goroutine.
func PickImages(ctx context.Context) ([]string, error) {
	var r struct {
		Paths []string `json:"paths"`
	}
	err := mygo.Native(pluginID).Call(ctx, "pickImages", nil, &r)
	return r.Paths, err
}

// Play plays the sound file at path, which the silent switch silences and
// other apps' audio keeps playing under.
func Play(ctx context.Context, path string) error {
	return mygo.Native(pluginID).Call(ctx, "play", map[string]any{"path": path}, nil)
}
