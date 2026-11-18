package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/domain/notification"
	domaintelegram "github.com/aognio/dns-change-monitor/pkg/domain/telegram"
)

// fakeAPI is a minimal stand in for the Telegram bot API.
type fakeAPI struct {
	server *httptest.Server

	mu       sync.Mutex
	updates  []map[string]any
	offset   int
	sent     []map[string]any
	meCalled int
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	api := &fakeAPI{}
	api.server = httptest.NewServer(http.HandlerFunc(api.handle))
	t.Cleanup(api.server.Close)
	return api
}

// handle answers the bot API calls the gateway and the notifier make.
func (f *fakeAPI) handle(writer http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) != 2 {
		http.Error(writer, "bad path", http.StatusBadRequest)
		return
	}
	method := parts[1]

	switch method {
	case "getMe":
		f.mu.Lock()
		f.meCalled++
		f.mu.Unlock()
		writeJSON(writer, map[string]any{
			"ok": true,
			"result": map[string]any{
				"id":         42,
				"is_bot":     true,
				"first_name": "Monitor",
				"username":   "monitorbot",
			},
		})
	case "getUpdates":
		f.mu.Lock()
		defer f.mu.Unlock()
		result := []map[string]any{}
		for _, update := range f.updates {
			if update["update_id"].(int) < f.offset {
				continue
			}
			f.offset = update["update_id"].(int) + 1
			result = append(result, update)
		}
		writeJSON(writer, map[string]any{"ok": true, "result": result})
	case "sendMessage":
		var payload map[string]any
		_ = json.NewDecoder(request.Body).Decode(&payload)
		f.mu.Lock()
		f.sent = append(f.sent, payload)
		f.mu.Unlock()
		writeJSON(writer, map[string]any{
			"ok": true,
			"result": map[string]any{
				"message_id": len(f.sent),
				"chat":       map[string]any{"id": chatIdentifier(payload["chat_id"]), "type": "private"},
				"text":       payload["text"],
			},
		})
	default:
		writeJSON(writer, map[string]any{"ok": false, "error_code": 404, "description": "unknown method"})
	}
}

// chatIdentifier renders the chat identifier of a send call as a number.
func chatIdentifier(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case string:
		number, _ := strconv.ParseInt(typed, 10, 64)
		return number
	default:
		return 0
	}
}

// queue adds an update that the next polls will deliver.
func (f *fakeAPI) queue(update map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, update)
}

// messages returns the texts of the messages sent through the bot.
func (f *fakeAPI) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	texts := make([]string, 0, len(f.sent))
	for _, payload := range f.sent {
		if text, ok := payload["text"].(string); ok {
			texts = append(texts, text)
		}
	}
	return texts
}

func writeJSON(writer http.ResponseWriter, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(payload)
}

// message builds an update carrying a text message.
func message(updateID int, chatID int64, username, text string) map[string]any {
	return map[string]any{
		"update_id": updateID,
		"message": map[string]any{
			"message_id": updateID,
			"from": map[string]any{
				"id":         chatID,
				"is_bot":     false,
				"first_name": "Tester",
				"username":   username,
			},
			"chat": map[string]any{
				"id":   chatID,
				"type": "private",
			},
			"date": time.Now().Unix(),
			"text": text,
		},
	}
}

// newTestGateway builds a gateway talking to the fake API.
func newTestGateway(t *testing.T, api *fakeAPI) *Gateway {
	t.Helper()
	gateway, err := New(Options{
		Token:       "test-token",
		PollTimeout: 50 * time.Millisecond,
		APIURL:      api.server.URL,
	})
	if err != nil {
		t.Fatalf("build gateway: %v", err)
	}
	t.Cleanup(gateway.Stop)
	return gateway
}

// waitCommand waits for the gateway to deliver a command.
func waitCommand(t *testing.T, commands <-chan domaintelegram.Command) domaintelegram.Command {
	t.Helper()
	select {
	case command, ok := <-commands:
		if !ok {
			t.Fatal("the command channel was closed")
		}
		return command
	case <-time.After(5 * time.Second):
		t.Fatal("no command was delivered")
		return domaintelegram.Command{}
	}
}

func TestListenDeliversTheStartCommand(t *testing.T) {
	api := newFakeAPI(t)
	gateway := newTestGateway(t, api)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	commands, err := gateway.Listen(ctx)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	api.queue(message(1, 100, "tester", "/start"))

	command := waitCommand(t, commands)
	if command.Command != "start" {
		t.Fatalf("got command %q, want start", command.Command)
	}
	if command.Payload != "" {
		t.Fatalf("got payload %q, want an empty payload", command.Payload)
	}
	if command.ChatID != 100 {
		t.Fatalf("got chat %d, want 100", command.ChatID)
	}
	if command.Username != "tester" {
		t.Fatalf("got username %q, want tester", command.Username)
	}
}

func TestListenDeliversTheStartCommandWithAToken(t *testing.T) {
	api := newFakeAPI(t)
	gateway := newTestGateway(t, api)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	commands, err := gateway.Listen(ctx)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	api.queue(message(2, 100, "tester", "/start abc-123"))

	command := waitCommand(t, commands)
	if command.Command != "start" {
		t.Fatalf("got command %q, want start", command.Command)
	}
	if command.Payload != "abc-123" {
		t.Fatalf("got payload %q, want abc-123", command.Payload)
	}
}

func TestListenIgnoresTheCommandsOfAnotherBot(t *testing.T) {
	api := newFakeAPI(t)
	gateway := newTestGateway(t, api)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	commands, err := gateway.Listen(ctx)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	api.queue(message(3, 100, "tester", "/start@otherbot abc-123"))

	select {
	case command := <-commands:
		t.Fatalf("got command %q, want nothing", command.Command)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestListenIgnoresTheMessagesThatAreNotCommands(t *testing.T) {
	api := newFakeAPI(t)
	gateway := newTestGateway(t, api)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	commands, err := gateway.Listen(ctx)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	api.queue(message(4, 100, "tester", "hello there"))

	select {
	case command := <-commands:
		t.Fatalf("got command %q, want nothing", command.Command)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestNotifierSendsTheMessage(t *testing.T) {
	api := newFakeAPI(t)
	gateway := newTestGateway(t, api)

	notifier := NewNotifier(gateway, nil)
	err := notifier.Send(context.Background(),
		notification.Recipient{ChatID: 100}, notification.Message{Text: "the bot works"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	messages := api.messages()
	if len(messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(messages))
	}
	if messages[0] != "the bot works" {
		t.Fatalf("got message %q, want the bot works", messages[0])
	}
}

func TestStopEndsThePolling(t *testing.T) {
	api := newFakeAPI(t)
	gateway := newTestGateway(t, api)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := gateway.Listen(ctx); err != nil {
		t.Fatalf("listen: %v", err)
	}

	gateway.Stop()
	gateway.Stop()
}

func TestNewRejectsAnEmptyToken(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("an empty token was accepted")
	}
}

func TestListenSurvivesAFailingAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/getMe") {
			writeJSON(writer, map[string]any{
				"ok": true,
				"result": map[string]any{
					"id": 42, "is_bot": true, "first_name": "Monitor", "username": "monitorbot",
				},
			})
			return
		}
		http.Error(writer, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	gateway, err := New(Options{
		Token:       "test-token",
		PollTimeout: 50 * time.Millisecond,
		APIURL:      server.URL,
	})
	if err != nil {
		t.Fatalf("build gateway: %v", err)
	}
	defer gateway.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := gateway.Listen(ctx); err != nil {
		t.Fatalf("listen: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
}
