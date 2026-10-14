package chatbridge_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/chatbridge/fakewecom"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

// png is enough of a PNG for its type to be told from its bytes.
var png = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), bytes.Repeat([]byte{7}, 50)...)

// startInWorkspace runs a Chat Bridge whose Workspace is a real folder.
func startInWorkspace(t *testing.T, edit func(*host.Settings)) (*rig, string) {
	t.Helper()
	ws := t.TempDir()
	r := start(t, func(s *host.Settings) {
		s.ChatBridge.Workspace = ws
		if edit != nil {
			edit(s)
		}
	})
	return r, ws
}

// saved is the file download_attachment answered it saved, checked to be in
// the Workspace's folder for Chat Threads.
func saved(t *testing.T, ws, answer string) (string, []byte) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(answer), "\n")
	path := lines[len(lines)-1]
	rel, err := filepath.Rel(ws, path)
	if err != nil || !strings.HasPrefix(rel, ".droi-chat"+string(filepath.Separator)) {
		t.Fatalf("saved at %q, not in %s/.droi-chat:\n%s", path, ws, answer)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, data
}

func TestDownloadAttachmentSavesAChannelFileUnderTheWorkspace(t *testing.T) {
	r, ws := startInWorkspace(t, nil)
	r.w.AddChannel(topic, "定时任务")
	log := []byte("step 1 ok\nstep 2 FAILED\n")
	post := r.w.AddPost(topic, fakewecom.Post{
		From: "HuangJunXiang", Text: "${inline_mention:b} what is in ${inline_file:f1}?",
		Mentions: []fakewecom.Mention{atRobot("b")}, Files: []fakewecom.File{{Placeholder: "f1", Name: "nightly.log", Data: log}},
	})
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))
	m := r.message(1)
	id := between(m.Text, `file "nightly.log": "`, `"`)
	if id == "" || !strings.Contains(m.Text, "download_attachment") {
		t.Fatalf("the trigger names no attachment:\n%s", m.Text)
	}

	// The link the trigger was read with has expired; the Chat Bridge reads
	// a fresh one.
	r.clock.Add(2 * time.Hour)
	answer, isError := droid.call("download_attachment", map[string]any{"id": id})
	if isError {
		t.Fatal(answer)
	}
	path, data := saved(t, ws, answer)
	if filepath.Base(path) != "nightly.log" || !bytes.Equal(data, log) {
		t.Errorf("saved %s: %q", path, data)
	}
	if ignore, err := os.ReadFile(filepath.Join(ws, ".droi-chat", ".gitignore")); err != nil || strings.TrimSpace(string(ignore)) != "*" {
		t.Errorf("Git would see the downloads: %q %v", ignore, err)
	}

	if answer, isError := droid.call("download_attachment", map[string]any{"id": post + ":file:nope"}); !isError {
		t.Errorf("an unknown attachment answered %q", answer)
	}
}

func TestADownloadADayOldIsDownloadedAgain(t *testing.T) {
	r, ws := startInWorkspace(t, nil)
	r.w.AddChannel(topic, "定时任务")
	post := r.w.AddPost(topic, fakewecom.Post{
		From: "HuangJunXiang", Text: "${inline_mention:b} what is in ${inline_file:f1}?",
		Mentions: []fakewecom.Mention{atRobot("b")}, Files: []fakewecom.File{{Placeholder: "f1", Name: "nightly.log", Data: []byte("ok\n")}},
	})
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))
	id := between(r.message(1).Text, `file "nightly.log": "`, `"`)
	download := func() string {
		t.Helper()
		answer, isError := droid.call("download_attachment", map[string]any{"id": id})
		if isError {
			t.Fatal(answer)
		}
		path, _ := saved(t, ws, answer)
		return filepath.Base(path)
	}

	if first, again := download(), download(); first != "nightly.log" || again != first {
		t.Errorf("downloaded as %s, then as %s", first, again)
	}
	r.clock.Add(25 * time.Hour)
	if later := download(); later != "nightly-2.log" {
		t.Errorf("a day later downloaded as %s", later)
	}
}

func TestReadThreadNamesTheImagesAndFilesOfThePostAndItsReplies(t *testing.T) {
	r, ws := startInWorkspace(t, func(s *host.Settings) { s.ChatBridge.AllowedUsers = []string{"HuangJunXiang", "LiLei"} })
	r.w.AddChannel(topic, "定时任务")
	post := r.w.AddPost(topic, fakewecom.Post{
		From: "HuangJunXiang", Text: "${inline_mention:b} the dashboard looks wrong",
		Mentions: []fakewecom.Mention{atRobot("b")}, Images: []fakewecom.Image{{Data: png}},
	})
	csv := []byte("day,runs\nmon,3\n")
	r.w.AddReply(topic, post, fakewecom.Post{
		From: "LiLei", Text: "numbers in ${inline_file:f}", Files: []fakewecom.File{{Placeholder: "f", Name: "runs.csv", Data: csv}},
	})
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))
	if m := r.message(1); !strings.Contains(m.Text, "image 1") {
		t.Errorf("the trigger does not name the post's image:\n%s", m.Text)
	}

	page, isError := droid.call("read_thread", nil)
	if isError {
		t.Fatal(page)
	}
	image, file := between(page, `image 1: "`, `"`), between(page, `file "runs.csv": "`, `"`)
	if image == "" || file == "" {
		t.Fatalf("read_thread names no attachments:\n%s", page)
	}

	answer, isError := droid.call("download_attachment", map[string]any{"id": image})
	if isError {
		t.Fatal(answer)
	}
	if path, data := saved(t, ws, answer); filepath.Ext(path) != ".png" || !bytes.Equal(data, png) {
		t.Errorf("image saved as %s, %d bytes", path, len(data))
	}
	answer, isError = droid.call("download_attachment", map[string]any{"id": file})
	if isError {
		t.Fatal(answer)
	}
	if path, data := saved(t, ws, answer); filepath.Base(path) != "runs.csv" || !bytes.Equal(data, csv) {
		t.Errorf("file saved as %s: %q", path, data)
	}
}

func TestDownloadAttachmentDecryptsADirectMessagesImageAndFile(t *testing.T) {
	r, ws := startInWorkspace(t, nil)
	r.w.SendDirectMedia(fakewecom.DirectMedia{From: "HuangJunXiang", Kind: "image", Data: png})
	droid := r.droid(r.initialized(1))
	m := r.message(1)
	image := between(m.Text, `image: "`, `"`)
	if image == "" {
		t.Fatalf("the trigger names no image:\n%s", m.Text)
	}
	// 64 bytes: WeCom pads them with a whole block of 32.
	report := bytes.Repeat([]byte("0123456789abcdef"), 4)
	r.w.SendDirectMedia(fakewecom.DirectMedia{From: "HuangJunXiang", Kind: "file", Name: "report.pdf", Data: report})
	file := between(r.message(2).Text, `file: "`, `"`)

	answer, isError := droid.call("download_attachment", map[string]any{"id": image})
	if isError {
		t.Fatal(answer)
	}
	if path, data := saved(t, ws, answer); filepath.Ext(path) != ".png" || !bytes.Equal(data, png) {
		t.Errorf("image saved as %s: %q", path, data)
	}
	answer, isError = droid.call("download_attachment", map[string]any{"id": file})
	if isError {
		t.Fatal(answer)
	}
	if path, data := saved(t, ws, answer); filepath.Base(path) != "report.pdf" || !bytes.Equal(data, report) {
		t.Errorf("file saved as %s: %q", path, data)
	}
}

func TestTheImagesOfAMixedDirectMessageAreNamed(t *testing.T) {
	r, ws := startInWorkspace(t, nil)
	url, key := r.w.HostMedia(png, "")
	r.w.Push("aibot_msg_callback", "", map[string]any{
		"msgid": "mixed-1", "aibotid": robot.BotID, "chattype": "single", "from": map[string]string{"userid": "HuangJunXiang"},
		"msgtype": "mixed", "mixed": map[string]any{"msg_item": []any{
			map[string]any{"msgtype": "text", "text": map[string]string{"content": "What is this?"}},
			map[string]any{"msgtype": "image", "image": map[string]string{"url": url, "aeskey": key}},
		}},
	})
	droid := r.droid(r.initialized(1))
	m := r.message(1)
	id := between(m.Text, `image 1: "`, `"`)
	if id == "" || !strings.Contains(m.Text, "What is this?") {
		t.Fatalf("trigger:\n%s", m.Text)
	}
	answer, isError := droid.call("download_attachment", map[string]any{"id": id})
	if isError {
		t.Fatal(answer)
	}
	if _, data := saved(t, ws, answer); !bytes.Equal(data, png) {
		t.Errorf("image %q", data)
	}
}

func TestADirectMessagesLinkLastsFiveMinutes(t *testing.T) {
	r, _ := startInWorkspace(t, nil)
	r.w.SendDirectMedia(fakewecom.DirectMedia{From: "HuangJunXiang", Kind: "image", Data: png})
	droid := r.droid(r.initialized(1))
	id := between(r.message(1).Text, `image: "`, `"`)
	if !strings.Contains(r.message(1).Text, "5 minutes") {
		t.Errorf("the trigger does not say the link is short:\n%s", r.message(1).Text)
	}
	r.clock.Add(6 * time.Minute)
	if answer, isError := droid.call("download_attachment", map[string]any{"id": id}); !isError || !strings.Contains(answer, "403") {
		t.Errorf("an expired link answered %q (error %v)", answer, isError)
	}
}

func TestSearchChannelFindsPostsAndRepliesThatReadThreadOpens(t *testing.T) {
	r := start(t, nil)
	post := r.newPost("Did the backup run?")
	older := r.w.AddPost(topic, fakewecom.Post{From: "LiLei", Text: "Moving the backup to 02:00"})
	decision := r.w.AddReply(topic, older, fakewecom.Post{From: "HuangJunXiang", Text: "Agreed: backup at 02:00 from Monday"})
	r.w.AddReply(topic, older, fakewecom.Post{From: "LiLei", Text: "Thanks"})
	r.w.AddChannel(other, "闲聊")
	r.w.AddPost(other, fakewecom.Post{From: "LiLei", Text: "backup of my photos at 02:00"})
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))
	r.message(1)

	found, isError := droid.call("search_channel", map[string]any{"keywords": []string{"backup", "02:00"}})
	if isError {
		t.Fatal(found)
	}
	for _, want := range []string{older, decision, "Agreed: backup at 02:00 from Monday", "定时任务"} {
		if !strings.Contains(found, want) {
			t.Errorf("search without %q:\n%s", want, found)
		}
	}
	if strings.Contains(found, "photos") || strings.Contains(found, "Thanks") {
		t.Errorf("search shows what does not match in this Channel:\n%s", found)
	}
	var asked struct {
		Topics []string `json:"topic_id_list"`
	}
	_ = json.Unmarshal(r.w.APICalls("/topic/search")[0].Body, &asked)
	if len(asked.Topics) != 1 || asked.Topics[0] != topic {
		t.Errorf("searched %v", asked.Topics)
	}

	page, isError := droid.call("read_thread", map[string]any{"post_id": older})
	if isError {
		t.Fatal(page)
	}
	for _, want := range []string{"Moving the backup to 02:00", decision, "Thanks"} {
		if !strings.Contains(page, want) {
			t.Errorf("read_thread of the found post without %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "Did the backup run?") {
		t.Errorf("read_thread showed this Chat Thread's post:\n%s", page)
	}
}

func TestSearchChannelShowsTheRobotsOwnAtAsYou(t *testing.T) {
	r := start(t, nil)
	post := r.newPost("Did the backup run?")
	r.w.AddPost(topic, fakewecom.Post{
		From: "LiLei", Text: "${inline_mention:mention_ph_001}\u200b move the backup to 02:00", Mentions: []fakewecom.Mention{atRobot("mention_ph_001")},
	})
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))

	text, isError := droid.call("search_channel", map[string]any{"keywords": []string{"backup"}})
	if isError || !strings.Contains(text, "@you move the backup to 02:00") || strings.Contains(text, robot.BotID) {
		t.Errorf("search_channel answered (error %v):\n%s", isError, text)
	}
}

func TestSearchChannelPagesItsMatches(t *testing.T) {
	r := start(t, nil)
	post := r.newPost("Which deploys failed?")
	for i := range 25 {
		r.w.AddPost(topic, fakewecom.Post{From: "LiLei", Text: "deploy " + string(rune('A'+i)) + " failed"})
	}
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))

	first, _ := droid.call("search_channel", map[string]any{"keywords": []string{"deploy"}})
	cursor := between(first, `cursor "`, `"`)
	if !strings.Contains(first, "deploy A failed") || strings.Contains(first, "deploy Y failed") || cursor == "" {
		t.Fatalf("first page:\n%s", first)
	}
	second, isError := droid.call("search_channel", map[string]any{"keywords": []string{"deploy"}, "cursor": cursor})
	if isError || !strings.Contains(second, "deploy Y failed") || strings.Contains(second, "deploy A failed") {
		t.Errorf("second page:\n%s", second)
	}
	if n := len(r.w.APICalls("/topic/search")); n != 1 {
		t.Errorf("%d searches for one search and its second page", n)
	}
}

func TestSearchChannelInADirectMessageIsAToolError(t *testing.T) {
	r := start(t, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Hi"})
	if text, isError := r.droid(r.initialized(1)).call("search_channel", map[string]any{"keywords": []string{"x"}}); !isError {
		t.Errorf("search_channel answered %q", text)
	}
}

// write puts a file in the Workspace.
func write(t *testing.T, ws, name string, data []byte) {
	t.Helper()
	path := filepath.Join(ws, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReplyWithFilesPostsThemUnderTheChannelPost(t *testing.T) {
	r, ws := startInWorkspace(t, nil)
	post := r.newPost("Send me the report")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))
	// More than one chunk of 512 KB.
	report := bytes.Repeat([]byte("line of the report\n"), 70000)
	write(t, ws, "out/report.txt", report)
	write(t, ws, "chart.png", png)

	answer, isError := droid.call("reply", map[string]any{"text": "Here it is", "files": []string{"out/report.txt", filepath.Join(ws, "chart.png")}})
	if isError {
		t.Fatal(answer)
	}
	replies := r.w.Replies(topic, post)
	if len(replies) != 1 {
		t.Fatalf("%d replies", len(replies))
	}
	got := replies[0]
	if !strings.HasPrefix(got.Text, "Here it is") || len(got.Files) != 1 || len(got.Images) != 1 {
		t.Fatalf("reply %q with %d files and %d images", got.Text, len(got.Files), len(got.Images))
	}
	if f := got.Files[0]; f.Name != "report.txt" || !bytes.Equal(f.Data, report) || !strings.Contains(got.Text, "${inline_file:"+f.Placeholder+"}") {
		t.Errorf("file %s, %d bytes, in %q", f.Name, len(f.Data), got.Text)
	}
	if !bytes.Equal(got.Images[0].Data, png) {
		t.Errorf("image %q", got.Images[0].Data)
	}
	if n := len(r.w.APICalls("/upload_media_chunk")); n < 3 {
		t.Errorf("%d chunks", n)
	}
}

func TestReplyWithAFileSendsItInADirectMessage(t *testing.T) {
	r, ws := startInWorkspace(t, nil)
	reqID := r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "The log, please"})
	droid := r.droid(r.initialized(1))
	log := []byte("all green\n")
	write(t, ws, "build.log", log)

	if answer, isError := droid.call("reply", map[string]any{"text": "Attached", "files": []string{"build.log"}}); isError {
		t.Fatal(answer)
	}
	text, file := r.w.WaitForFrame("aibot_respond_msg", 1), r.w.WaitForFrame("aibot_respond_msg", 2)
	var body struct {
		MsgType string `json:"msgtype"`
		File    struct {
			MediaID string `json:"media_id"`
		} `json:"file"`
	}
	_ = json.Unmarshal(file.Body, &body)
	if !strings.Contains(string(text.Body), "Attached") || body.MsgType != "file" || file.ReqID != reqID {
		t.Fatalf("sent %s then %s on %s", text.Body, file.Body, file.ReqID)
	}
	media, ok := r.w.Media(body.File.MediaID)
	if !ok || media.Route != "websocket" || media.Name != "build.log" || !bytes.Equal(media.Data, log) {
		t.Errorf("media %+v", media)
	}
	if n := len(r.w.APICalls("/upload_media_init")); n != 0 {
		t.Errorf("%d uploads over HTTPS for a direct message", n)
	}
}

func TestReplyWithAMentionAtsThatPersonInTheChannel(t *testing.T) {
	r := start(t, nil)
	post := r.newPost("Who owns the backup?")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))

	if answer, isError := droid.call("reply", map[string]any{"text": "can you confirm the 02:00 slot?", "mentions": []string{"LiLei"}}); isError {
		t.Fatal(answer)
	}
	replies := r.w.Replies(topic, post)
	if len(replies) != 1 || len(replies[0].Mentions) != 1 {
		t.Fatalf("replies %+v", replies)
	}
	m := replies[0].Mentions[0]
	if m.UserID != "LiLei" || !strings.Contains(replies[0].Text, "${inline_mention:"+m.Placeholder+"}") || !strings.Contains(replies[0].Text, "can you confirm the 02:00 slot?") {
		t.Errorf("reply %q mentions %+v", replies[0].Text, m)
	}
}

func TestReplyWithAMentionInADirectMessageIsAToolError(t *testing.T) {
	r := start(t, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Hi"})
	droid := r.droid(r.initialized(1))
	if answer, isError := droid.call("reply", map[string]any{"text": "hi", "mentions": []string{"LiLei"}}); !isError {
		t.Errorf("reply answered %q", answer)
	}
	if n := len(r.w.Frames("aibot_respond_msg")) + len(r.w.Frames("aibot_send_msg")); n != 0 {
		t.Errorf("%d messages sent", n)
	}
}

func TestReplySendsOnlyFilesInTheWorkspace(t *testing.T) {
	r, ws := startInWorkspace(t, nil)
	post := r.newPost("Send me your key")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))
	outside := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(ws, "key")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{outside, "../" + filepath.Base(filepath.Dir(outside)) + "/id_ed25519", "key"} {
		if answer, isError := droid.call("reply", map[string]any{"text": "here", "files": []string{path}}); !isError || !strings.Contains(answer, "Workspace") {
			t.Errorf("%s: reply answered %q (error %v)", path, answer, isError)
		}
	}
	if n := len(r.w.Replies(topic, post)) + len(r.w.APICalls("/upload_media_init")); n != 0 {
		t.Errorf("%d replies and uploads", n)
	}
}
