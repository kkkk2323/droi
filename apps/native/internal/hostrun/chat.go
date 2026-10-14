package hostrun

import (
	"log"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/internal/chatbridge"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

// StartChatBridge is the Host's Chat Bridge (ADR 0020), connected while the
// settings name a robot. Like Memory Sessions it dials the Daemon directly
// with the Host's credential.
func StartChatBridge(h *host.Host) *chatbridge.Bridge {
	b := chatbridge.New(chatbridge.Options{
		Settings: h.Settings,
		Daemon: controller.Config{
			ResolveURL:           h.WaitForDaemonURL,
			Credential:           h.Credential,
			Caller:               "droi-native",
			MaxReconnectAttempts: 1000,
		},
		OnChange: h.Changed,
		Log:      func(s string) { log.Println("[chat]", s) },
	})
	h.OnChange(b.Follow)
	b.Follow()
	return b
}
