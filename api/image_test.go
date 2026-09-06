package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"fmt"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestHEICDisplay(t *testing.T) {
	for _, orientation := range []int{2, 6} {
		t.Run(fmt.Sprint(orientation), func(t *testing.T) {
			path, err := filepath.Abs(fmt.Sprintf("testdata/orientation-%d.heic", orientation))
			if err != nil {
				t.Fatal(err)
			}
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			d := fixture(t, func(db *sql.DB) {
				if _, err := db.Exec("UPDATE attachment SET filename=?,transfer_name='fixture.heic',mime_type='image/heic'", path); err != nil {
					t.Fatal(err)
				}
			})
			page := get[Message](t, d, "/messages/"+opaque("iMessage;-;alex@example.invalid")+"?limit=1")
			a := page.Items[0].Attachments[0]
			if a.DataBase64 == nil || *a.DataBase64 != base64.StdEncoding.EncodeToString(original) {
				t.Fatal("original changed")
			}
			if a.DisplayDataBase64 == nil {
				t.Fatal("missing display JPEG")
			}
			data, err := base64.StdEncoding.DecodeString(*a.DisplayDataBase64)
			if err != nil {
				t.Fatal(err)
			}
			img, err := jpeg.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			w, h := 2048, 1536
			if orientation == 6 {
				w, h = h, w
			}
			if img.Bounds().Dx() != w || img.Bounds().Dy() != h {
				t.Fatal(img.Bounds())
			}
			// Mirror puts green at top-left; clockwise rotation puts blue there.
			r, g, b, _ := img.At(100, 100).RGBA()
			if orientation == 2 && (g < 50000 || r > 10000 || b > 10000) {
				t.Fatalf("mirror: %d %d %d", r, g, b)
			}
			if orientation == 6 && (b < 50000 || r > 10000 || g > 10000) {
				t.Fatalf("rotation: %d %d %d", r, g, b)
			}
			if displayImage(data) != nil {
				t.Fatal("JPEG should not be converted")
			}
		})
	}
	if displayImage([]byte("invalid")) != nil || displayImage([]byte("0000ftypheic")) != nil {
		t.Fatal("invalid image accepted")
	}
}
