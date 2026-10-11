package secretbox

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func testKey(fill byte) []byte { return bytes.Repeat([]byte{fill}, KeySize) }

func mustBox(t *testing.T, key []byte) *Box {
	t.Helper()
	b, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSealOpenRoundTrip(t *testing.T) {
	b := mustBox(t, testKey(1))
	sealed, err := b.Seal("1//refresh-token")
	if err != nil {
		t.Fatal(err)
	}
	if !IsSealed(sealed) || strings.Contains(sealed, "refresh-token") {
		t.Fatalf("sealed value leaks plaintext or lacks prefix: %q", sealed)
	}
	again, _ := b.Seal("1//refresh-token")
	if again == sealed {
		t.Fatal("expected a random nonce per seal")
	}
	opened, err := b.Open(sealed)
	if err != nil || opened != "1//refresh-token" {
		t.Fatalf("Open = %q, %v", opened, err)
	}
}

func TestOpenPassesThroughLegacyPlaintext(t *testing.T) {
	b := mustBox(t, testKey(1))
	for _, box := range []*Box{b, nil} {
		got, err := box.Open("ya29.plain")
		if err != nil || got != "ya29.plain" {
			t.Fatalf("Open(plaintext) = %q, %v", got, err)
		}
	}
}

func TestOpenRejectsWrongKeyTamperingAndMissingKey(t *testing.T) {
	sealed, _ := mustBox(t, testKey(1)).Seal("secret")
	if _, err := mustBox(t, testKey(2)).Open(sealed); err == nil {
		t.Fatal("expected wrong key to fail")
	}
	tampered := sealed[:len(sealed)-2] + "AA"
	if _, err := mustBox(t, testKey(1)).Open(tampered); err == nil {
		t.Fatal("expected tampered value to fail")
	}
	var none *Box
	if _, err := none.Open(sealed); !errors.Is(err, ErrNoKey) {
		t.Fatalf("expected ErrNoKey, got %v", err)
	}
	if _, err := mustBox(t, testKey(1)).Open(Prefix + "!!"); err == nil {
		t.Fatal("expected invalid encoding to fail")
	}
}

func TestPtrHelpers(t *testing.T) {
	b := mustBox(t, testKey(3))
	if got, _ := b.SealPtr(nil); got != nil {
		t.Fatal("SealPtr(nil) should be nil")
	}
	empty := ""
	if got, _ := b.SealPtr(&empty); got == nil || *got != "" {
		t.Fatal("empty values are stored unchanged")
	}
	plain := "token"
	sealed, err := b.SealPtr(&plain)
	if err != nil || !IsSealed(*sealed) {
		t.Fatalf("SealPtr = %v, %v", sealed, err)
	}
	if resealed, _ := b.SealPtr(sealed); *resealed != *sealed {
		t.Fatal("already sealed values must not be sealed twice")
	}
	var none *Box
	if got, _ := none.SealPtr(&plain); *got != "token" {
		t.Fatal("nil Box stores plaintext")
	}
	opened, err := b.OpenPtr(sealed)
	if err != nil || *opened != "token" {
		t.Fatalf("OpenPtr = %v, %v", opened, err)
	}
	if got, _ := b.OpenPtr(nil); got != nil {
		t.Fatal("OpenPtr(nil) should be nil")
	}
}

func TestParseKey(t *testing.T) {
	key := testKey(7)
	for name, raw := range map[string]string{
		"hex":        hex.EncodeToString(key),
		"base64":     base64.StdEncoding.EncodeToString(key),
		"base64 raw": base64.RawURLEncoding.EncodeToString(key),
		"whitespace": "  " + base64.StdEncoding.EncodeToString(key) + "\n",
	} {
		got, err := ParseKey(raw)
		if err != nil || !bytes.Equal(got, key) {
			t.Errorf("%s: ParseKey = %x, %v", name, got, err)
		}
	}
	for _, bad := range []string{"", "short", base64.StdEncoding.EncodeToString(testKey(1)[:16]), strings.Repeat("z", 64)} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) should fail", bad)
		}
	}
	if _, err := New(testKey(1)[:16]); err == nil {
		t.Error("New should reject 16-byte keys")
	}
}
