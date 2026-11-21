package discord

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient() *Client {
	return &Client{Attempts: 3, MaxWait: 20 * time.Millisecond, Backoff: time.Millisecond}
}

func ok(w http.ResponseWriter) {
	w.Write([]byte(`{"id":"m1","channel_id":"c1"}`))
}

func TestMultipartShape(t *testing.T) {
	var gotQuery string
	var payloadJSON map[string]any
	files := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mt != "multipart/form-data" {
			t.Errorf("content type %s", mt)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			if p.FormName() == "payload_json" {
				json.Unmarshal(b, &payloadJSON)
			} else {
				files[p.FormName()+":"+p.FileName()] = string(b)
			}
		}
		ok(w)
	}))
	defer srv.Close()

	res, err := testClient().Send(context.Background(), srv.URL+"/api/webhooks/1/tok", Message{
		Username:   "ACME - Ada",
		Content:    "hi",
		Embeds:     []Embed{{Title: "t", Image: "attachment://shot.png", Timestamp: time.Unix(0, 0)}},
		Files:      []File{{Name: "shot.png", ContentType: "image/png", Data: []byte("PNG")}},
		ThreadName: "run 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.MessageID != "m1" || res.ChannelID != "c1" {
		t.Fatalf("result %+v", res)
	}
	if !strings.Contains(gotQuery, "wait=true") {
		t.Fatalf("query %q", gotQuery)
	}
	if files["files[0]:shot.png"] != "PNG" {
		t.Fatalf("files %v", files)
	}
	if payloadJSON["thread_name"] != "run 1" || payloadJSON["username"] != "ACME - Ada" {
		t.Fatalf("payload %v", payloadJSON)
	}
	emb := payloadJSON["embeds"].([]any)[0].(map[string]any)
	if emb["image"].(map[string]any)["url"] != "attachment://shot.png" {
		t.Fatalf("embed %v", emb)
	}
	att := payloadJSON["attachments"].([]any)[0].(map[string]any)
	if att["filename"] != "shot.png" {
		t.Fatalf("attachments %v", att)
	}
}

func TestMentionSuppression(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		ok(w)
	}))
	defer srv.Close()

	send := func(role string) map[string]any {
		_, err := testClient().Send(context.Background(), srv.URL+"/w/1/t", Message{Content: "@everyone <@123>", RoleID: role})
		if err != nil {
			t.Fatal(err)
		}
		return body["allowed_mentions"].(map[string]any)
	}
	am := send("")
	if p, isList := am["parse"].([]any); !isList || len(p) != 0 {
		t.Fatalf("parse %v", am["parse"])
	}
	if _, has := am["roles"]; has {
		t.Fatal("roles should be absent")
	}
	am = send("555")
	if r := am["roles"].([]any); len(r) != 1 || r[0] != "555" {
		t.Fatalf("roles %v", am["roles"])
	}
	if !strings.HasPrefix(body["content"].(string), "<@&555>") {
		t.Fatalf("content %q", body["content"])
	}
	am = send("everyone")
	if _, has := am["roles"]; has {
		t.Fatal("bad role id accepted")
	}
}

func TestTrimmingOnTheWire(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		ok(w)
	}))
	defer srv.Close()
	_, err := testClient().Send(context.Background(), srv.URL+"/w/1/t", Message{
		Username: "Discord " + strings.Repeat("x", 100),
		Content:  strings.Repeat("c", 3000),
		Embeds:   []Embed{{Title: strings.Repeat("t", 300), Description: strings.Repeat("d", 5000)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(body["username"].(string))); n > 80 || strings.Contains(strings.ToLower(body["username"].(string)), "discord") {
		t.Fatalf("username %q", body["username"])
	}
	if len([]rune(body["content"].(string))) != 2000 {
		t.Fatal("content not trimmed")
	}
	e := body["embeds"].([]any)[0].(map[string]any)
	if len([]rune(e["title"].(string))) != 256 || len([]rune(e["description"].(string))) != 4096 {
		t.Fatal("embed not trimmed")
	}
}

func TestSkippedFilesFallBackToLinks(t *testing.T) {
	var contentType string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&body)
		ok(w)
	}))
	defer srv.Close()
	_, err := testClient().Send(context.Background(), srv.URL+"/w/1/t", Message{
		Content: "x",
		Embeds:  []Embed{{Title: "t", Image: "attachment://big.png"}},
		Files:   []File{{Name: "big.png", Data: make([]byte, maxFileSize+1), URL: "https://hub/big.png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "application/json" {
		t.Fatalf("content type %s", contentType)
	}
	if !strings.Contains(body["content"].(string), "https://hub/big.png") {
		t.Fatalf("content %q", body["content"])
	}
	e := body["embeds"].([]any)[0].(map[string]any)
	if e["image"].(map[string]any)["url"] != "https://hub/big.png" {
		t.Fatalf("image %v", e["image"])
	}
}

func TestFileCountLimit(t *testing.T) {
	p := prepare(Message{Content: "x", Files: func() []File {
		var fs []File
		for i := 0; i < 12; i++ {
			fs = append(fs, File{Name: "f" + string(rune('a'+i)), Data: []byte("d"), URL: "https://hub/f"})
		}
		return fs
	}()}, false)
	if len(p.files) != maxFiles {
		t.Fatalf("kept %d", len(p.files))
	}
	if strings.Count(p.payload.Content, "https://hub/f") != 2 {
		t.Fatalf("content %q", p.payload.Content)
	}
}

func TestRetryOn429(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.WriteHeader(429)
			w.Write([]byte(`{"retry_after":0.005}`))
			return
		}
		ok(w)
	}))
	defer srv.Close()
	if _, err := testClient().Send(context.Background(), srv.URL+"/w/1/t", Message{Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("calls %d", n)
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.WriteHeader(429)
			w.Write([]byte(`{"retry_after":3600}`))
			return
		}
		ok(w)
	}))
	defer srv.Close()
	start := time.Now()
	if _, err := testClient().Send(context.Background(), srv.URL+"/w/1/t", Message{Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("wait was not capped")
	}
}

func TestServerErrorRetriesThenFails(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(502)
	}))
	defer srv.Close()
	_, err := testClient().Send(context.Background(), srv.URL+"/w/1/t", Message{Content: "x"})
	if err == nil || !IsRetryable(err) || n != 3 {
		t.Fatalf("err %v calls %d", err, n)
	}
}

func TestWebhookGone(t *testing.T) {
	for _, code := range []int{401, 404} {
		var n int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&n, 1)
			w.WriteHeader(code)
		}))
		_, err := testClient().Send(context.Background(), srv.URL+"/w/1/secrettoken", Message{Content: "x"})
		srv.Close()
		if !errors.Is(err, ErrWebhookGone) || n != 1 {
			t.Fatalf("%d: err %v calls %d", code, err, n)
		}
		if strings.Contains(err.Error(), "secrettoken") {
			t.Fatal("token in error")
		}
	}
}

func TestPermanent4xx(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(400)
		w.Write([]byte(`{"message":"Invalid Form Body"}`))
	}))
	defer srv.Close()
	_, err := testClient().Send(context.Background(), srv.URL+"/w/1/t", Message{Content: "x"})
	var se *StatusError
	if !errors.As(err, &se) || se.Retryable || n != 1 {
		t.Fatalf("err %v calls %d", err, n)
	}
}

func TestTransportErrorHidesURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()
	c := testClient()
	c.Attempts = 1
	_, err := c.Send(context.Background(), addr+"/w/1/secrettoken", Message{Content: "x"})
	if err == nil || strings.Contains(err.Error(), "secrettoken") || !IsRetryable(err) {
		t.Fatalf("err %v", err)
	}
	if _, err := c.Send(context.Background(), "::bad::", Message{Content: "x"}); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("err %v", err)
	}
}

func TestButtonsSentWithComponents(t *testing.T) {
	var q string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.RawQuery
		json.NewDecoder(r.Body).Decode(&body)
		ok(w)
	}))
	defer srv.Close()
	_, err := testClient().Send(context.Background(), srv.URL+"/w/1/t", Message{
		Content: "x",
		Buttons: []Button{{Label: "Open", URL: "https://hub/a"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, "with_components=true") {
		t.Fatalf("query %q", q)
	}
	b := body["components"].([]any)[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	if b["style"].(float64) != 5 || b["url"] != "https://hub/a" {
		t.Fatalf("button %v", b)
	}
}

func TestNoComponentsFallback(t *testing.T) {
	var calls []string
	var last map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.RawQuery)
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		if _, has := b["components"]; has {
			w.WriteHeader(400)
			w.Write([]byte(`{"message":"components not allowed"}`))
			return
		}
		last = b
		ok(w)
	}))
	defer srv.Close()
	_, err := testClient().Send(context.Background(), srv.URL+"/w/1/t", Message{
		Embeds:  []Embed{{Title: "t"}},
		Buttons: []Button{{Label: "Open", URL: "https://hub/a"}, {Label: "Artifact", URL: "https://hub/b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || strings.Contains(calls[1], "with_components") {
		t.Fatalf("calls %v", calls)
	}
	f := last["embeds"].([]any)[0].(map[string]any)["fields"].([]any)[0].(map[string]any)
	v := f["value"].(string)
	if f["name"] != "Links" || !strings.Contains(v, "[Open](https://hub/a)") || !strings.Contains(v, "[Artifact](https://hub/b)") {
		t.Fatalf("field %v", f)
	}
}

func TestEmptyMessage(t *testing.T) {
	if _, err := testClient().Send(context.Background(), "https://example.com/w/1/t", Message{}); !errors.Is(err, ErrEmpty) {
		t.Fatalf("err %v", err)
	}
}
