package controller_test

import (
	"context"
	"fmt"
	"log"
	"os"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"
)

// A Session started, a message sent, permission prompts answered and the
// reply read from the store.
func Example() {
	ctx := context.Background()
	ctl := controller.New(controller.Config{
		URL: "ws://127.0.0.1:37643", // `droid daemon --port 37643`
		Credential: func(context.Context) (*droid.Credential, error) {
			return &droid.Credential{APIKey: os.Getenv("FACTORY_API_KEY")}, nil
		},
	})
	defer ctl.Close()
	if err := ctl.Connect(ctx); err != nil {
		log.Fatal(err)
	}
	done := make(chan struct{})
	ctl.Subscribe(func(e controller.Event) {
		switch e := e.(type) {
		case controller.PermissionRequested:
			ctl.RespondToPermission(ctx, e.Permission.RequestID, controller.PermissionAnswer{
				SelectedOption: protocol.ToolConfirmationOutcomeProceedOnce,
			})
		case controller.SessionNotification:
			if _, ok := e.Value.(*protocol.AgentTurnCompletedNotification); ok {
				close(done)
			}
		}
	})
	// The store re-renders on every change; a UI subscribes here.
	ctl.Store().Subscribe(func(e session.Event) {})

	res, err := ctl.InitializeSession(ctx, protocol.InitializeSessionParams{Cwd: "/path/to/repo"})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := ctl.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: res.SessionID, Text: "Run the tests"}); err != nil {
		log.Fatal(err)
	}
	<-done
	for _, m := range ctl.Store().Session(res.SessionID).DisplayMessages() {
		fmt.Println(m.Role, len(m.Content))
	}

	// Methods the Controller does not wrap are on the current connection.
	cl, err := ctl.Client()
	if err != nil {
		log.Fatal(err)
	}
	diff, err := cl.GetGitDiff(ctx, protocol.GetGitDiffParams{SessionID: res.SessionID})
	if err == nil && diff.Success {
		fmt.Println("workspace has a git diff")
	}
}
