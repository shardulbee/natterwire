package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func (d *database) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	respond := func(status int, v any) { w.WriteHeader(status); _ = json.NewEncoder(w).Encode(v) }
	fail := func(status int, message string) { respond(status, map[string]string{"error": message}) }
	if r.Method != http.MethodGet {
		fail(405, "method not allowed")
		return
	}
	q := r.URL.Query()
	limit := 50
	if q.Has("limit") {
		var err error
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil || limit < 1 || limit > 100 {
			fail(400, "limit must be an integer between 1 and 100")
			return
		}
	}
	var before *string
	if q.Has("before") {
		s := q.Get("before")
		before = &s
	}
	parts := strings.FieldsFunc(r.URL.Path, func(r rune) bool { return r == '/' })
	metadataOnly := q.Get("media") == "metadata"
	isMedia := (len(parts) == 2 && parts[0] == "attachments") || (strings.Contains(r.URL.Path, "/messages") && !metadataOnly)
	if isMedia {
		select {
		case d.mediaSlots <- struct{}{}:
			defer func() { <-d.mediaSlots }()
		default:
			fail(503, "media busy")
			return
		}
	}
	if len(parts) == 2 && parts[0] == "attachments" {
		path, found, err := d.attachmentPath(r.Context(), parts[1])
		if err != nil {
			fail(500, "database query failed")
			return
		}
		if !found {
			fail(404, "not found")
			return
		}
		media, err := d.media.response(parts[1], path, q.Get("version"))
		if err != nil {
			switch {
			case errors.Is(err, errMediaNotFound):
				fail(404, "not found")
			case errors.Is(err, errMediaChanged):
				fail(409, "attachment version changed")
			case errors.Is(err, errMediaTooLarge):
				fail(413, "attachment too large")
			default:
				fail(404, "not found")
			}
			return
		}
		w.Header().Set("Content-Type", media.contentType)
		w.WriteHeader(200)
		_, _ = w.Write(media.body)
		return
	}
	var result any
	var err error
	switch {
	case len(parts) == 1 && parts[0] == "chats":
		result, err = d.chats(r.Context(), limit, before)
	case len(parts) == 3 && parts[0] == "chats" && parts[2] == "messages":
		result, err = d.messages(r.Context(), parts[1], limit, before, metadataOnly)
	case len(parts) == 2 && parts[0] == "messages":
		result, err = d.messages(r.Context(), parts[1], limit, before, metadataOnly)
	default:
		fail(404, "not found")
		return
	}
	if err != nil {
		if errors.Is(err, errIdentifier) || errors.Is(err, errCursor) {
			fail(400, err.Error())
		} else {
			fail(500, "database query failed")
		}
		return
	}
	respond(200, result)
}

func main() {
	path := os.Getenv("NATTERWIRE_DB_PATH")
	if path == "" {
		path = os.Getenv("MESSAGES_DB_PATH")
	}
	if path == "" {
		path = "~/Library/Messages/chat.db"
	}
	dbPath := flag.String("db", path, "Messages SQLite database (opened read-only)")
	port := flag.Int("port", 8741, "loopback HTTP port")
	contacts := flag.String("contacts", "~/.config/natterwire/contacts.json", "handle-to-name JSON dictionary; empty disables")
	pinsPath := flag.String("pins", "~/Library/Preferences/com.apple.messages.pinning.plist", "Messages pinning plist; empty disables")
	flag.Parse()
	if flag.NArg() != 0 || *port < 1 || *port > 65535 {
		flag.Usage()
		os.Exit(2)
	}
	var contactsSet, pinsSet bool
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "contacts" {
			contactsSet = true
		}
		if f.Name == "pins" {
			pinsSet = true
		}
	})
	var n names
	var pins []string
	var err error
	if *contacts != "" {
		n, err = loadNames(*contacts)
		if err != nil && (contactsSet || !errors.Is(err, os.ErrNotExist)) {
			log.Fatalf("contacts: %v", err)
		}
	}
	if *pinsPath != "" {
		pins, err = loadPins(*pinsPath)
		if err != nil {
			if pinsSet {
				log.Fatalf("pins: %v", err)
			}
			if !errors.Is(err, os.ErrNotExist) {
				log.Print("pinning preferences unavailable; using activity order")
			}
		}
	}
	d, err := openDatabase(*dbPath, n, pins)
	if err != nil {
		log.Fatalf("cannot open Messages database: %v. On macOS grant Full Disk Access to this executable or its responsible launcher, then restart; the old Swift app's grant does not transfer", err)
	}
	defer d.db.Close()
	server := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", *port), Handler: d, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("listening on http://%s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-done
}
