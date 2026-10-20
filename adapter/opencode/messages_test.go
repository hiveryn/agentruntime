package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hiveryn/agentruntime"
)

func TestPrepareLaunchMessageDeliveryAddsPortAndPassword(t *testing.T) {
	adapter := New(DefaultOptions())
	spec, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{
		ID: "s1", Workdir: "/tmp/work",
		MessageDelivery: &agentruntime.MessageDelivery{ServerPort: 43123, ServerPassword: "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "--port 43123") {
		t.Fatalf("args %q", spec.Args)
	}
	if spec.Env[serverPasswordEnv] != "secret" {
		t.Fatalf("env %v", spec.Env)
	}
	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "s1", Workdir: "/tmp/work", MessageDelivery: &agentruntime.MessageDelivery{}}); err == nil {
		t.Fatal("missing port/password accepted")
	}
	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "s1", Workdir: "/tmp/work", Args: []string{"--port", "1"}}); err == nil {
		t.Fatal("--port in raw args accepted")
	}
	plain, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "s1", Workdir: "/tmp/work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.Env[serverPasswordEnv]; ok || strings.Contains(strings.Join(plain.Args, " "), "--port") {
		t.Fatalf("plain launch leaked delivery plumbing: %q %v", plain.Args, plain.Env)
	}
}

type fakeServer struct {
	mu       sync.Mutex
	password string
	messages map[string]bool
	posts    []map[string]any
}

func (f *fakeServer) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "opencode" || pass != f.password {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/session/ses_1/message/"):
			id := strings.TrimPrefix(r.URL.Path, "/session/ses_1/message/")
			if f.messages[id] {
				_, _ = w.Write([]byte(`{"info":{"id":"` + id + `"}}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/session/ses_1/prompt_async":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.posts = append(f.posts, body)
			f.messages[body["messageID"].(string)] = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestDeliverMessagePostsOnceAndNeverResendsAnExistingID(t *testing.T) {
	adapter := New(DefaultOptions())
	fake := &fakeServer{password: "pw", messages: map[string]bool{}}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	portNum := server.Listener.Addr().(*net.TCPAddr).Port
	req := agentruntime.DeliverRequest{NativeSessionID: "ses_1", ServerPort: portNum, ServerPassword: "pw", MessageID: "m-1", Text: "Hiveryn message m-1: hello"}

	res, err := adapter.DeliverMessage(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != agentruntime.DeliverAccepted || !regexp.MustCompile(`^msg_[0-9a-f]{12}[0-9A-Za-z]{14}$`).MatchString(res.ProviderRef) {
		t.Fatalf("result %+v", res)
	}
	if len(fake.posts) != 1 || fake.posts[0]["parts"].([]any)[0].(map[string]any)["text"] != "Hiveryn message m-1: hello" {
		t.Fatalf("posts %+v", fake.posts)
	}
	// Retry with the provider ref: found, nothing re-sent.
	req.ProviderRef = res.ProviderRef
	again, err := adapter.DeliverMessage(context.Background(), req)
	if err != nil || again.Outcome != agentruntime.DeliverAlreadyPresent || again.ProviderRef != res.ProviderRef || len(fake.posts) != 1 {
		t.Fatalf("retry %+v %v posts %d", again, err, len(fake.posts))
	}
	// A ref that never landed gets a fresh, later id.
	req.ProviderRef = "msg_000000000000AAAAAAAAAAAAAA"
	fresh, err := adapter.DeliverMessage(context.Background(), req)
	if err != nil || fresh.Outcome != agentruntime.DeliverAccepted || fresh.ProviderRef == req.ProviderRef || len(fake.posts) != 2 {
		t.Fatalf("fresh %+v %v posts %d", fresh, err, len(fake.posts))
	}
	// Wrong password is an error, not a silent miss.
	req.ServerPassword = "nope"
	if _, err := adapter.DeliverMessage(context.Background(), req); err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("password err = %v", err)
	}
	req.ServerPassword = "pw"
	req.ServerPort = 0
	if _, err := adapter.DeliverMessage(context.Background(), req); err == nil {
		t.Fatal("missing port accepted")
	}
}

func TestAscendingMessageIDsSortByTime(t *testing.T) {
	a, err := newAscendingMessageID(time.UnixMilli(1_000_000))
	if err != nil {
		t.Fatal(err)
	}
	b, err := newAscendingMessageID(time.UnixMilli(1_000_001))
	if err != nil {
		t.Fatal(err)
	}
	if a >= b || len(a) != 4+12+14 {
		t.Fatalf("ids %q %q", a, b)
	}
}

func TestConfirmMessageReadsDatabase(t *testing.T) {
	adapter := New(DefaultOptions())
	dbPath := newTestDB(t, t.TempDir())
	insertSession(t, dbPath, "ses_1")
	handle := dbPath + pathSep + "ses_1"
	insert := func(id, role, parent string, order int, completed ...int64) {
		row := map[string]any{"role": role, "parentID": parent}
		if len(completed) > 0 {
			row["time"] = map[string]any{"created": completed[0] - 1, "completed": completed[0]}
		}
		data, _ := json.Marshal(row)
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		if _, err := db.Exec(`INSERT INTO message (id, session_id, time_created, data) VALUES (?,?,?,?)`, id, "ses_1", order, string(data)); err != nil {
			t.Fatal(err)
		}
	}
	insert("msg_a", "user", "", 1)
	insert("msg_b", "assistant", "msg_a", 2, 1000)
	receipt, err := adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: handle, MessageID: "m-1", ProviderRef: "msg_c"})
	if err != nil || receipt.Appeared {
		t.Fatalf("absent = %+v %v", receipt, err)
	}
	insert("msg_c", "user", "", 3)
	receipt, err = adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: handle, MessageID: "m-1", ProviderRef: "msg_c"})
	if err != nil || !receipt.Appeared || receipt.Acknowledged {
		t.Fatalf("appeared = %+v %v", receipt, err)
	}
	// The assistant row exists as soon as OpenCode starts replying: not a
	// reply until it is completed.
	insert("msg_d", "assistant", "msg_c", 4)
	receipt, err = adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: handle, MessageID: "m-1", ProviderRef: "msg_c"})
	if err != nil || !receipt.Appeared || receipt.Acknowledged {
		t.Fatalf("reply in progress = %+v %v", receipt, err)
	}
	insert("msg_e", "assistant", "msg_c", 5, 2000)
	receipt, err = adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: handle, MessageID: "m-1", ProviderRef: "msg_c"})
	if err != nil || !receipt.Appeared || !receipt.Acknowledged {
		t.Fatalf("acknowledged = %+v %v", receipt, err)
	}
	if receipt, err := adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: handle, MessageID: "m-1"}); err != nil || receipt.Appeared {
		t.Fatalf("no provider ref = %+v %v", receipt, err)
	}
	if cap := adapter.MessageCapability(); cap.Mode != agentruntime.MessageDeliveryPush || !cap.GateOnIdle {
		t.Fatalf("capability %+v", cap)
	}
}
