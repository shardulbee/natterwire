package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

type Chat struct {
	ID            string  `json:"id"`
	DisplayName   string  `json:"displayName"`
	Service       *string `json:"service,omitempty"`
	LastMessageAt *string `json:"lastMessageAt,omitempty"`
	MessageCount  int64   `json:"messageCount"`
}
type Message struct {
	ID          string       `json:"id"`
	Text        string       `json:"text"`
	SentAt      *string      `json:"sentAt,omitempty"`
	IsFromMe    bool         `json:"isFromMe"`
	Sender      *string      `json:"sender,omitempty"`
	Service     *string      `json:"service,omitempty"`
	Attachments []Attachment `json:"attachments"`
}
type Attachment struct {
	ID                string  `json:"id"`
	Filename          *string `json:"filename,omitempty"`
	MimeType          *string `json:"mimeType,omitempty"`
	MediaID           *string `json:"mediaID,omitempty"`
	Version           *string `json:"version,omitempty"`
	Width             *int    `json:"width,omitempty"`
	Height            *int    `json:"height,omitempty"`
	DataBase64        *string `json:"dataBase64,omitempty"`
	DisplayDataBase64 *string `json:"displayDataBase64,omitempty"`
	path              string
}
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextBefore *string `json:"nextBefore"`
}

type database struct {
	db            *sql.DB
	message, chat map[string]bool
	tables        map[string]bool
	names         names
	pins          []string
	media         *attachmentMedia
	mediaSlots    chan struct{}
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func openDatabase(path string, contacts names, pins []string) (*database, error) {
	path, err := filepath.Abs(expandHome(path))
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=busy_timeout(3000)&_pragma=query_only(1)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	// Finish each result set before querying names or attachments on this connection.
	db.SetMaxOpenConns(1)
	d := &database{db: db, names: contacts, tables: map[string]bool{}, media: newAttachmentMedia(128 << 20), mediaSlots: make(chan struct{}, 2)}
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table'")
	if err != nil {
		db.Close()
		return nil, err
	}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			break
		}
		d.tables[name] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err == nil {
		d.message, err = d.columns("message")
	}
	if err == nil {
		d.chat, err = d.columns("chat")
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	for _, p := range pins {
		if p != "" && !slices.Contains(d.pins, p) {
			d.pins = append(d.pins, p)
		}
	}
	return d, nil
}

func (d *database) columns(table string) (map[string]bool, error) {
	rows, err := d.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var id, notnull, pk int
		var name, typ string
		var def any
		if err := rows.Scan(&id, &name, &typ, &notnull, &def, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("missing required table: %s", table)
	}
	return cols, rows.Err()
}
func column(cols map[string]bool, alias, name string) string {
	if cols[name] {
		return alias + "." + name
	}
	return "NULL"
}
func (d *database) hasAttachments() bool {
	return d.tables["attachment"] && d.tables["message_attachment_join"]
}
func (d *database) messageFilter() string {
	bodies := []string{"m.text IS NOT NULL"}
	if d.message["attributedBody"] {
		bodies = append(bodies, "m.attributedBody IS NOT NULL")
	}
	if d.hasAttachments() {
		bodies = append(bodies, "EXISTS (SELECT 1 FROM message_attachment_join maj WHERE maj.message_id=m.ROWID)")
	}
	filters := []string{"m.date IS NOT NULL", "(" + strings.Join(bodies, " OR ") + ")"}
	for _, c := range []string{"item_type", "group_action_type", "associated_message_type", "is_deleted"} {
		if d.message[c] {
			filters = append(filters, "COALESCE(m."+c+",0)=0")
		}
	}
	return strings.Join(filters, " AND ")
}

var errIdentifier = errors.New("invalid chat identifier")
var errCursor = errors.New("invalid pagination cursor")

func opaque(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
func unopaque(s string) (string, error) {
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	s += strings.Repeat("=", (4-len(s)%4)%4)
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || !utf8.Valid(b) {
		return "", errIdentifier
	}
	return string(b), nil
}

type cursor struct {
	pin, date, row int64
	ranked         bool
}

func parseCursor(before *string, ranked bool) (*cursor, error) {
	if before == nil {
		return nil, nil
	}
	s, err := unopaque(*before)
	if err != nil {
		return nil, errCursor
	}
	p := strings.Split(s, ":")
	c := &cursor{}
	if len(p) == 4 && p[0] == "v2" && ranked {
		c.ranked = true
		c.pin, err = strconv.ParseInt(p[1], 10, 64)
		p = p[2:]
	}
	if err != nil || len(p) != 2 {
		return nil, errCursor
	}
	c.date, err = strconv.ParseInt(p[0], 10, 64)
	if err != nil {
		return nil, errCursor
	}
	c.row, err = strconv.ParseInt(p[1], 10, 64)
	if err != nil {
		return nil, errCursor
	}
	return c, nil
}
func dateString(raw int64) *string {
	if raw <= 0 {
		return nil
	}
	seconds := raw
	if raw > 10_000_000_000 {
		seconds = raw / 1_000_000_000
	}
	s := time.Unix(seconds+978307200, 0).UTC().Format(time.RFC3339)
	return &s
}

func (d *database) chats(ctx context.Context, limit int, before *string) (Page[Chat], error) {
	page := Page[Chat]{Items: []Chat{}}
	c, err := parseCursor(before, true)
	if err != nil {
		return page, err
	}
	args := []any{}
	pinCols := []string{}
	for _, col := range []string{"chat_identifier", "group_id"} {
		if d.chat[col] {
			pinCols = append(pinCols, "c."+col+" = ?")
		}
	}
	pinExpr := "0"
	if len(pinCols) > 0 && len(d.pins) > 0 {
		pinExpr = "CASE "
		for i, p := range d.pins {
			pinExpr += fmt.Sprintf("WHEN %s THEN %d ", strings.Join(pinCols, " OR "), i)
			for range pinCols {
				args = append(args, p)
			}
		}
		pinExpr += fmt.Sprintf("ELSE %d END", len(d.pins))
	}
	filters := d.messageFilter()
	for _, col := range []string{"is_archived", "is_deleted"} {
		if d.chat[col] {
			filters += " AND COALESCE(c." + col + ",0)=0"
		}
	}
	count := "0"
	if d.tables["chat_handle_join"] {
		count = "(SELECT COUNT(*) FROM chat_handle_join chj WHERE chj.chat_id=c.ROWID)"
	}
	having := ""
	order := "pin_order ASC, MAX(m.date) DESC, c.ROWID DESC"
	if c != nil {
		having = "HAVING MAX(m.date) < ? OR (MAX(m.date) = ? AND c.ROWID < ?)"
		if c.ranked {
			having = "HAVING pin_order > ? OR (pin_order = ? AND (MAX(m.date) < ? OR (MAX(m.date) = ? AND c.ROWID < ?)))"
			args = append(args, c.pin, c.pin)
		} else {
			order = "MAX(m.date) DESC, c.ROWID DESC"
		}
		args = append(args, c.date, c.date, c.row)
	}
	query := fmt.Sprintf(`SELECT c.guid, %s, %s, MAX(m.date), COUNT(m.ROWID), c.ROWID, %s, %s, %s AS pin_order
	 FROM chat c JOIN chat_message_join cmj ON cmj.chat_id=c.ROWID JOIN message m ON m.ROWID=cmj.message_id
	 WHERE c.guid IS NOT NULL AND %s GROUP BY c.ROWID %s ORDER BY %s LIMIT ?`, column(d.chat, "c", "display_name"), column(d.chat, "c", "service_name"), column(d.chat, "c", "chat_identifier"), count, pinExpr, filters, having, order)
	args = append(args, limit+1)
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	type chatRow struct {
		chat                         Chat
		guid                         string
		explicit, identifier         *string
		date, row, participants, pin int64
	}
	values := []chatRow{}
	for rows.Next() {
		var v chatRow
		err = rows.Scan(&v.guid, &v.explicit, &v.chat.Service, &v.date, &v.chat.MessageCount, &v.row, &v.identifier, &v.participants, &v.pin)
		if err != nil {
			break
		}
		v.chat.ID = opaque(v.guid)
		v.chat.LastMessageAt = dateString(v.date)
		values = append(values, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(values) > limit {
		values = values[:limit]
		last := values[len(values)-1]
		next := fmt.Sprintf("%d:%d", last.date, last.row)
		if c == nil || c.ranked {
			next = fmt.Sprintf("v2:%d:%s", last.pin, next)
		}
		next = opaque(next)
		page.NextBefore = &next
	}
	for _, v := range values {
		v.chat.DisplayName, err = d.chatName(ctx, v.explicit, v.guid, v.identifier, v.row, v.participants)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, v.chat)
	}
	return page, nil
}

func (d *database) chatName(ctx context.Context, explicit *string, guid string, identifier *string, row, participants int64) (string, error) {
	if explicit != nil && strings.TrimSpace(*explicit) == "" {
		explicit = nil
	}
	if strings.Contains(guid, ";+;") || participants > 1 {
		if explicit != nil {
			return *explicit, nil
		}
		if !d.tables["chat_handle_join"] || !d.tables["handle"] {
			return "Group chat", nil
		}
		rows, err := d.db.QueryContext(ctx, `SELECT h.id FROM chat_handle_join chj JOIN handle h ON h.ROWID=chj.handle_id WHERE chj.chat_id=? ORDER BY chj.ROWID`, row)
		if err != nil {
			return "", err
		}
		defer rows.Close()
		names := []string{}
		for rows.Next() {
			var h *string
			if err := rows.Scan(&h); err != nil {
				return "", err
			}
			if h == nil || strings.TrimSpace(*h) == "" {
				continue
			}
			name := strings.TrimSpace(*h)
			if n := d.names.lookup(name); n != "" {
				name = n
			}
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
		if err := rows.Err(); err != nil {
			return "", err
		}
		if len(names) == 0 {
			return "Group chat", nil
		}
		return strings.Join(names, ", "), nil
	}
	handle := ""
	if identifier != nil {
		handle = strings.TrimSpace(*identifier)
	}
	if handle == "" {
		if _, s, ok := strings.Cut(guid, ";-;"); ok {
			handle = strings.TrimSpace(s)
		}
	}
	if name := d.names.lookup(handle); name != "" {
		return name, nil
	}
	if explicit != nil {
		return *explicit, nil
	}
	if handle != "" {
		return handle, nil
	}
	return "Chat", nil
}

func (d *database) messages(ctx context.Context, id string, limit int, before *string, metadataOnly ...bool) (Page[Message], error) {
	page := Page[Message]{Items: []Message{}}
	guid, err := unopaque(id)
	if err != nil {
		return page, errIdentifier
	}
	c, err := parseCursor(before, false)
	if err != nil {
		return page, err
	}
	sender, join := "NULL", ""
	if d.message["handle_id"] {
		sender = "h.id"
		join = "LEFT JOIN handle h ON h.ROWID=m.handle_id"
	}
	args := []any{guid}
	clause := ""
	if c != nil {
		clause = "AND (m.date < ? OR (m.date = ? AND m.ROWID < ?))"
		args = append(args, c.date, c.date, c.row)
	}
	args = append(args, limit+1)
	query := fmt.Sprintf(`SELECT m.guid,m.text,%s,m.date,COALESCE(m.is_from_me,0),%s,%s,m.ROWID
	 FROM chat c JOIN chat_message_join cmj ON cmj.chat_id=c.ROWID JOIN message m ON m.ROWID=cmj.message_id %s
	 WHERE c.guid=? AND %s %s ORDER BY m.date DESC,m.ROWID DESC LIMIT ?`, column(d.message, "m", "attributedBody"), sender, column(d.message, "m", "service"), join, d.messageFilter(), clause)
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	type messageRow struct {
		message   Message
		row, date int64
	}
	values := []messageRow{}
	for rows.Next() {
		var v messageRow
		var id, text *string
		var body []byte
		var fromMe int
		err = rows.Scan(&id, &text, &body, &v.date, &fromMe, &v.message.Sender, &v.message.Service, &v.row)
		if err != nil {
			break
		}
		v.message.ID = strconv.FormatInt(v.row, 10)
		if id != nil {
			v.message.ID = *id
		}
		if text != nil {
			v.message.Text = *text
		} else {
			v.message.Text, _ = decodeBody(body)
		}
		v.message.IsFromMe = fromMe != 0
		v.message.SentAt = dateString(v.date)
		if v.message.Sender != nil {
			if n := d.names.lookup(*v.message.Sender); n != "" {
				v.message.Sender = &n
			}
		}
		values = append(values, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(values) > limit {
		values = values[:limit]
		last := values[len(values)-1]
		next := opaque(fmt.Sprintf("%d:%d", last.date, last.row))
		page.NextBefore = &next
	}
	for _, v := range values {
		v.message.Attachments, err = d.attachments(ctx, v.row)
		if err != nil {
			return page, err
		}
		for i := range v.message.Attachments {
			v.message.Attachments[i] = d.media.populate(v.message.Attachments[i], len(metadataOnly) > 0 && metadataOnly[0])
		}
		page.Items = append(page.Items, v.message)
	}
	return page, nil
}

func (d *database) attachments(ctx context.Context, row int64) ([]Attachment, error) {
	result := []Attachment{}
	if !d.hasAttachments() {
		return result, nil
	}
	rows, err := d.db.QueryContext(ctx, `SELECT a.guid,a.filename,a.transfer_name,a.mime_type,a.ROWID FROM message_attachment_join maj JOIN attachment a ON a.ROWID=maj.attachment_id WHERE maj.message_id=? ORDER BY a.ROWID`, row)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Attachment
		var id, path *string
		var row int64
		if err := rows.Scan(&id, &path, &a.Filename, &a.MimeType, &row); err != nil {
			return nil, err
		}
		a.ID = strconv.FormatInt(row, 10)
		if id != nil {
			a.ID = *id
		}
		mediaID := opaque("attachment:" + strconv.FormatInt(row, 10))
		a.MediaID = &mediaID
		if path != nil {
			p := expandHome(*path)
			a.path = p
			if a.Filename == nil {
				name := filepath.Base(p)
				a.Filename = &name
			}
		}
		result = append(result, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (d *database) attachmentPath(ctx context.Context, id string) (string, bool, error) {
	decoded, err := unopaque(id)
	if err != nil || !strings.HasPrefix(decoded, "attachment:") {
		return "", false, nil
	}
	row, err := strconv.ParseInt(strings.TrimPrefix(decoded, "attachment:"), 10, 64)
	if err != nil || row <= 0 || opaque("attachment:"+strconv.FormatInt(row, 10)) != id {
		return "", false, nil
	}
	if !d.hasAttachments() {
		return "", false, nil
	}
	var path *string
	err = d.db.QueryRowContext(ctx, "SELECT filename FROM attachment WHERE ROWID=?", row).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) || path == nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return expandHome(*path), true, nil
}
