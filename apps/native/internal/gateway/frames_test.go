package gateway

import (
	"testing"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
)

func TestInjectCredentialTouchesOnlyThePlaceholderFields(t *testing.T) {
	key := &droid.Credential{APIKey: "fk-1"}
	login := &droid.Credential{Token: "jwt"}
	for _, c := range []struct {
		name  string
		frame string
		cred  *droid.Credential
		want  string
	}{
		{"api key into apiKey", `{"id":"1","params":{"apiKey":"droi-gateway","caller":"sdk"}}`, key, `{"id":"1","params":{"apiKey":"fk-1","caller":"sdk"}}`},
		{"login token moves apiKey to token", `{"params":{"apiKey":"droi-gateway","caller":"sdk"}}`, login, `{"params":{"caller":"sdk","token":"jwt"}}`},
		{"spawn token", `{"params":{"cwd":"/x","token":"droi-gateway"}}`, key, `{"params":{"cwd":"/x","token":"fk-1"}}`},
		{"no credential", `{"params":{"apiKey":"droi-gateway"}}`, nil, `{"params":{"apiKey":"droi-gateway"}}`},
		{"placeholder elsewhere", `{"params":{"text":"droi-gateway"}}`, key, `{"params":{"text":"droi-gateway"}}`},
		{"nested placeholder", `{"params":{"x":{"apiKey":"droi-gateway"}}}`, key, `{"params":{"x":{"apiKey":"droi-gateway"}}}`},
		{"not json", `droi-gateway`, key, `droi-gateway`},
		{"params not an object", `{"params":"droi-gateway"}`, key, `{"params":"droi-gateway"}`},
		{"html stays unescaped", `{"params":{"apiKey":"droi-gateway","q":"<a&b>"}}`, &droid.Credential{APIKey: "k<&>"}, `{"params":{"apiKey":"k<&>","q":"<a&b>"}}`},
	} {
		if got := string(InjectCredential([]byte(c.frame), c.cred)); got != c.want {
			t.Errorf("%s: %s", c.name, got)
		}
	}
}

func TestInjectSystemPrompt(t *testing.T) {
	for _, c := range []struct {
		name, frame, addition, want string
	}{
		{"adds the preset", `{"method":"daemon.initialize_session","params":{"cwd":"/x"}}`, "Be brief", `{"method":"daemon.initialize_session","params":{"cwd":"/x","systemPrompt":{"type":"preset","preset":"droid","append":"Be brief"}}}`},
		{"blank addition", `{"method":"daemon.initialize_session","params":{}}`, " \n", `{"method":"daemon.initialize_session","params":{}}`},
		{"other method", `{"method":"daemon.load_session","params":{}}`, "x", `{"method":"daemon.load_session","params":{}}`},
		{"own prompt", `{"method":"daemon.initialize_session","params":{"systemPrompt":null}}`, "x", `{"method":"daemon.initialize_session","params":{"systemPrompt":null}}`},
		{"own override", `{"method":"daemon.initialize_session","params":{"systemPromptOverride":"y"}}`, "x", `{"method":"daemon.initialize_session","params":{"systemPromptOverride":"y"}}`},
	} {
		if got := string(InjectSystemPrompt([]byte(c.frame), c.addition)); got != c.want {
			t.Errorf("%s: %s", c.name, got)
		}
	}
}
