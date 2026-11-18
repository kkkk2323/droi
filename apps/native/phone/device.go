package main

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"github.com/kkkk2323/droi/apps/native/phone/phonekit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

//go:embed sounds/*.wav
var sounds embed.FS

// writeSounds puts the alert sounds where iOS can play them from, once.
func writeSounds(dir string) {
	_ = os.MkdirAll(dir, 0o755)
	for _, name := range []string{"finished.wav", "needs-input.wav"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if b, err := sounds.ReadFile("sounds/" + name); err == nil {
			_ = os.WriteFile(path, b, 0o644)
		}
	}
}

// alerted is a Session finishing, or starting to wait for an answer: a
// haptic always, and its sound when that is on.
func (p *phone) alerted(waiting, sound bool) {
	name := "finished.wav"
	if waiting {
		mygo.Haptics.Warning()
		name = "needs-input.wav"
	} else {
		mygo.Haptics.Success()
	}
	if sound {
		go func() { _ = phonekit.Play(context.Background(), filepath.Join(p.userData, "sounds", name)) }()
	}
}

func pickImages() ([]string, error) {
	return phonekit.PickImages(context.Background())
}

func scanQR() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	return phonekit.ScanQR(ctx, l10n.L("Point the camera at the pairing code in Droi → Settings → Remote Access on your computer."), l10n.L("Cancel"))
}
