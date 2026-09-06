package main

import "testing"

func TestContactSource(t *testing.T) {
	n, err := parseNames([]byte(`{" ALEX@example.invalid ":" Alex ","+1 (415) 555-0100":"Sam"}`))
	if err != nil {
		t.Fatal(err)
	}
	d := fixture(t, nil)
	d.nativeName = n.lookup
	page := get[Chat](t, d, "/chats")
	if page.Items[0].DisplayName != "Alex, Sam, unknown@example.invalid" || page.Items[1].DisplayName != "Sam" || page.Items[2].DisplayName != "Alex" {
		t.Fatalf("native names not used: %+v", page.Items)
	}
	// Simulate an updated snapshot, then permission revocation.
	d.nativeName = func(string) string { return "Updated" }
	if get[Chat](t, d, "/chats").Items[2].DisplayName != "Updated" {
		t.Fatal("stale names")
	}
	d.nativeName = func(string) string { return "" }
	if get[Chat](t, d, "/chats").Items[2].DisplayName != "Stale name" {
		t.Fatal("missing fallback")
	}
	d.nativeName = nil
	if d.contactName("alex@example.invalid") != "Alex Chen" {
		t.Fatal("JSON override lost")
	}
}

func TestNativeContactsOutsideBundle(t *testing.T) {
	// Tests are bare executables with no Contacts usage description. They must
	// never request permission or read personal contacts, even on a Mac.
	if nativeContacts() != nil {
		t.Fatal("native Contacts enabled in bare test executable")
	}
}
