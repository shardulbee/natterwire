package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed web/index.html web/shell.css web/shell.js web/InterVariable.woff2 web/favicon.png web/apple-touch-icon.png
var webFiles embed.FS

func webHandler(api http.Handler) http.Handler {
	assets, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	for _, pattern := range []string{"/attachments/", "/chats", "/chats/", "/messages/", "/send-session"} {
		mux.Handle(pattern, api)
	}
	mux.Handle("/", http.FileServer(http.FS(assets)))
	root := http.NewServeMux()
	root.Handle("/natterwire/", http.StripPrefix("/natterwire", mux))
	root.Handle("/", mux)
	return root
}

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
		result, err = d.chats(r.Context(), limit, before, q.Get("sort") == "latest")
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
	contacts := flag.String("contacts", "", "handle-to-name JSON override; empty disables; default uses macOS Contacts")
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
		if err != nil {
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
	var lookup func(string) string
	if !contactsSet {
		lookup = nativeContacts()
	}
	run := func(ctx context.Context, ready func()) error {
		d, err := openDatabase(*dbPath, n, pins)
		if err != nil {
			return fmt.Errorf("%w: %v", errMessagesAccess, err)
		}
		defer d.db.Close()
		d.nativeName = lookup
		return serve(ctx, fmt.Sprintf("127.0.0.1:%d", *port), webHandler(newSendAPI(d, sendText)), ready)
	}
	if flag.NFlag() == 0 && nativeApplication(run) {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, func() { log.Printf("listening on http://127.0.0.1:%d", *port) }); err != nil {
		log.Fatal(err)
	}
}

var errMessagesAccess = errors.New("Messages database unavailable; enable Full Disk Access for Natterwire.app")

func serve(ctx context.Context, address string, handler http.Handler, ready func()) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if server.Shutdown(shutdown) != nil {
				_ = server.Close()
			}
		case <-done:
		}
	}()
	ready()
	err = server.Serve(listener)
	close(done)
	<-stopped
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
