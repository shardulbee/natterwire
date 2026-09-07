package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"howett.net/plist"
)

func fixture(t *testing.T, modify func(*sql.DB)) *database {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chat.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("testdata/messages.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(b)); err != nil {
		t.Fatal(err)
	}
	if modify != nil {
		modify(db)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	n, err := loadNames("testdata/contacts.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := openDatabase(path, n, []string{"group-pin", "group-pin", "", "+1 (415) 555-0100"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.db.Close() })
	return d
}
func get[T any](t *testing.T, d *database, target string) Page[T] {
	t.Helper()
	w := httptest.NewRecorder()
	d.ServeHTTP(w, httptest.NewRequest("GET", target, nil))
	if w.Code != 200 {
		t.Fatalf("%s: %d %s", target, w.Code, w.Body.String())
	}
	var p Page[T]
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Items == nil {
		t.Fatal("items must be an array")
	}
	return p
}
func TestChatOrderingAndNames(t *testing.T) {
	d := fixture(t, nil)
	p := get[Chat](t, d, "/chats?limit=2")
	if len(p.Items) != 2 || p.Items[0].DisplayName != "Alex Chen, Sam Rivera, unknown@example.invalid" || p.Items[1].DisplayName != "Sam Rivera" || p.NextBefore == nil {
		t.Fatalf("%+v", p)
	}
	if *p.NextBefore != opaque("v2:1:600000000000000000:3") {
		t.Fatal(*p.NextBefore)
	}
	p = get[Chat](t, d, "/chats?limit=2&before="+*p.NextBefore)
	if len(p.Items) != 2 || p.Items[0].DisplayName != "Alex Chen" || p.Items[0].MessageCount != 6 || p.Items[1].DisplayName != "Group chat" || p.NextBefore != nil {
		t.Fatalf("%+v", p)
	}
	legacy := get[Chat](t, d, "/chats?limit=1&before="+opaque("800000003000000000:1"))
	if legacy.Items[0].ID != opaque("iMessage;+;weekend") || *legacy.NextBefore != opaque("700000000000000000:2") {
		t.Fatalf("%+v", legacy)
	}
	d.names = nil
	p = get[Chat](t, d, "/chats")
	if p.Items[0].DisplayName != "alex@example.invalid, +1 (415) 555-0100, unknown@example.invalid" || p.Items[2].DisplayName != "Stale name" {
		t.Fatalf("%+v", p)
	}
}
func TestLatestChatOrdering(t *testing.T) {
	d := fixture(t, nil)
	var ids []string
	target := "/chats?sort=latest&limit=1"
	for len(ids) < 5 {
		p := get[Chat](t, d, target)
		for _, chat := range p.Items {
			ids = append(ids, chat.ID)
		}
		if p.NextBefore == nil {
			break
		}
		target = "/chats?sort=latest&limit=1&before=" + *p.NextBefore
	}
	want := []string{opaque("iMessage;-;alex@example.invalid"), opaque("iMessage;+;weekend"), opaque("any;-;+1 (415) 555-0100"), opaque("iMessage;+;empty-group")}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("latest ordering with pins: got %v, want %v", ids, want)
	}
}

func TestMessagePaginationAndBodies(t *testing.T) {
	d := fixture(t, nil)
	target := "/chats/" + opaque("iMessage;-;alex@example.invalid") + "/messages?limit=2"
	var all []Message
	next := ""
	for {
		p := get[Message](t, d, target+next)
		all = append(all, p.Items...)
		if p.NextBefore == nil {
			break
		}
		next = "&before=" + *p.NextBefore
		if len(all) > 10 {
			t.Fatal("pagination did not advance")
		}
	}
	ids := []string{}
	for _, m := range all {
		ids = append(ids, m.ID)
		if m.Attachments == nil {
			t.Fatal("attachments must be an array")
		}
	}
	if !reflect.DeepEqual(ids, []string{"attachment-only", "invalid-body", "archived-body", "second", "first", "empty-body"}) {
		t.Fatalf("%v", ids)
	}
	if all[1].Text != "" || all[2].Text != "Archived message" || all[5].Text != "" || !all[3].IsFromMe || *all[2].Sender != "Alex Chen" {
		t.Fatalf("%+v", all)
	}
	alias := get[Message](t, d, "/messages/"+opaque("iMessage;-;alex@example.invalid"))
	if !reflect.DeepEqual(alias.Items, all) {
		t.Fatal("alias differs")
	}
	unknown := get[Message](t, d, "/messages/"+opaque("missing"))
	if len(unknown.Items) != 0 || unknown.NextBefore != nil {
		t.Fatal(unknown)
	}
}

func TestDefaultLimitAndPlainTextPrecedence(t *testing.T) {
	d := fixture(t, func(db *sql.DB) {
		if _, err := db.Exec("UPDATE message SET text='' WHERE guid='archived-body'"); err != nil {
			t.Fatal(err)
		}
		for i := 20; i < 125; i++ {
			if _, err := db.Exec("INSERT INTO message (ROWID,guid,text,date,is_from_me) VALUES (?,?,?,100,0)", i, fmt.Sprint(i), "tail"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("INSERT INTO chat_message_join VALUES (1,?)", i); err != nil {
				t.Fatal(err)
			}
		}
	})
	target := "/messages/" + opaque("iMessage;-;alex@example.invalid")
	p := get[Message](t, d, target)
	if len(p.Items) != 50 || p.NextBefore == nil || p.Items[2].ID != "archived-body" || p.Items[2].Text != "" {
		t.Fatalf("default page: %+v", p)
	}
	if p := get[Message](t, d, target+"?limit=100"); len(p.Items) != 100 || p.NextBefore == nil {
		t.Fatal("maximum limit")
	}
}

func TestGroupClassification(t *testing.T) {
	d := fixture(t, func(db *sql.DB) {
		if _, err := db.Exec("UPDATE chat SET guid='iMessage;-;looks-direct', display_name='Named group' WHERE ROWID=2"); err != nil {
			t.Fatal(err)
		}
	})
	p := get[Chat](t, d, "/chats")
	if p.Items[0].DisplayName != "Named group" {
		t.Fatal(p)
	}
	name, err := d.chatName(context.Background(), nil, "iMessage;-;looks-direct", nil, 2, 4)
	if err != nil || name != "Alex Chen, Sam Rivera, unknown@example.invalid" {
		t.Fatal(name, err)
	}
}

func TestHTTPValidation(t *testing.T) {
	d := fixture(t, nil)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"POST", "/chats", 405}, {"HEAD", "/chats", 405}, {"GET", "/nope", 404},
		{"GET", "/chats?limit=", 400}, {"GET", "/chats?limit=0", 400}, {"GET", "/chats?limit=101", 400}, {"GET", "/chats?limit=1.5", 400}, {"GET", "/chats?limit=999999999999999999999", 400},
		{"GET", "/chats?before=", 400}, {"GET", "/chats?before=oops", 400}, {"GET", "/messages/!", 400},
		{"GET", "/messages/" + opaque("x") + "?before=" + opaque("v2:0:1:2"), 400},
		{"GET", "/chats?limit=1&limit=bad", 200}, {"GET", "/chats?limit=100", 200}, {"GET", "/chats", 200},
	} {
		t.Run(tc.path+tc.method, func(t *testing.T) {
			w := httptest.NewRecorder()
			d.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Content-Type") != "application/json" || !json.Valid(w.Body.Bytes()) {
				t.Fatal(w)
			}
		})
	}
	d.db.Close()
	w := httptest.NewRecorder()
	d.ServeHTTP(w, httptest.NewRequest("GET", "/chats", nil))
	if w.Code != 500 || strings.Contains(w.Body.String(), "sqlite") {
		t.Fatal(w)
	}
}
func TestAttachments(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.png")
	if err := os.WriteFile(small, []byte("image bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.png")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate((10 << 20) + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	d := fixture(t, func(db *sql.DB) {
		for i, path := range []string{small, big, filepath.Join(dir, "missing.png")} {
			if _, err := db.Exec("INSERT INTO attachment VALUES (?,NULL,?,NULL,NULL)", i+2, path); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("INSERT INTO message_attachment_join VALUES (14,?)", i+2); err != nil {
				t.Fatal(err)
			}
		}
	})
	p := get[Message](t, d, "/messages/"+opaque("iMessage;-;alex@example.invalid")+"?limit=1")
	a := p.Items[0].Attachments
	if len(a) != 4 || a[0].DataBase64 != nil || a[1].ID != "2" || *a[1].Filename != "small.png" || *a[1].DataBase64 != base64.StdEncoding.EncodeToString([]byte("image bytes")) || a[2].DataBase64 != nil || a[3].DataBase64 != nil {
		t.Fatalf("%+v", a)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), dir) {
		t.Fatal("attachment path leaked")
	}
}
func TestReadOnlyAndMinimalSchema(t *testing.T) {
	d := fixture(t, nil)
	if _, err := d.db.Exec("DELETE FROM message"); err == nil {
		t.Fatal("database writable")
	}
	path := filepath.Join(t.TempDir(), "missing.db")
	if _, err := openDatabase(path, nil, nil); err == nil {
		t.Fatal("opened missing database")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("created missing database")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE chat (guid TEXT); CREATE TABLE message (guid TEXT,text TEXT,date INTEGER,is_from_me INTEGER); CREATE TABLE chat_message_join(chat_id INTEGER,message_id INTEGER); INSERT INTO chat VALUES ('iMessage;-;minimal'); INSERT INTO message VALUES(NULL,'',100,NULL); INSERT INTO chat_message_join VALUES(1,1);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	minimal, err := openDatabase(path, nil, []string{"ignored"})
	if err != nil {
		t.Fatal(err)
	}
	defer minimal.db.Close()
	p := get[Chat](t, minimal, "/chats")
	if p.Items[0].DisplayName != "minimal" || p.Items[0].Service != nil {
		t.Fatal(p)
	}
	m := get[Message](t, minimal, "/messages/"+p.Items[0].ID)
	if m.Items[0].ID != "1" || m.Items[0].Text != "" || *m.Items[0].SentAt != "2001-01-01T00:01:40Z" {
		t.Fatal(m)
	}
}
func TestNamesAndPins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contacts.json")
	os.WriteFile(path, []byte(`{"USER@Example.invalid":" User ","+1 (415) 555-0100":"Sam","x":"ignored"}`), 0600)
	n, err := loadNames(path)
	if err != nil {
		t.Fatal(err)
	}
	for h, want := range map[string]string{"user@example.invalid": "User", "4155550100": "Sam", "+14155550100": "Sam", "123": "", "unknown@example.invalid": ""} {
		if got := n.lookup(h); got != want {
			t.Fatalf("%s: %q", h, got)
		}
	}
	for _, format := range []int{plist.XMLFormat, plist.BinaryFormat} {
		b, err := plist.Marshal(map[string]any{"pD": map[string]any{"pP": []string{"a", "b"}}}, format)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(path, b, 0600)
		pins, err := loadPins(path)
		if err != nil || !reflect.DeepEqual(pins, []string{"a", "b"}) {
			t.Fatal(pins, err)
		}
	}
	if dateString(0) != nil || dateString(-1) != nil || *dateString(100) != *dateString(100_000_000_000) {
		t.Fatal("date conversion")
	}
}
func TestConcurrentHTTP(t *testing.T) {
	d := fixture(t, nil)
	server := httptest.NewServer(d)
	defer server.Close()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			res, err := http.Get(server.URL + "/chats")
			if err != nil {
				t.Error(err)
				return
			}
			defer res.Body.Close()
			if res.StatusCode != 200 {
				t.Error(res.Status)
			}
		})
	}
	wg.Wait()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.chats(ctx, 50, nil, false); err == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestChatTies(t *testing.T) {
	d := fixture(t, func(db *sql.DB) {
		if _, err := db.Exec("UPDATE message SET date=100"); err != nil {
			t.Fatal(err)
		}
	})
	d.pins = nil
	var ids []string
	var before *string
	for {
		p, err := d.chats(context.Background(), 1, before, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range p.Items {
			ids = append(ids, c.ID)
		}
		if p.NextBefore == nil {
			break
		}
		before = p.NextBefore
		if len(ids) > 6 {
			t.Fatal("cursor loop")
		}
	}
	want := []string{}
	for _, guid := range []string{"iMessage;+;empty-group", "any;-;+1 (415) 555-0100", "iMessage;+;weekend", "iMessage;-;alex@example.invalid"} {
		want = append(want, opaque(guid))
	}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Fatal(ids)
	}
}
