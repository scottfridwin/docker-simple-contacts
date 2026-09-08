package person

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type scanStub struct {
	values []any
	err    error
}

func (s scanStub) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	for i, value := range s.values {
		switch d := dest[i].(type) {
		case *uuid.UUID:
			*d = value.(uuid.UUID)
		case *string:
			*d = value.(string)
		case *[]string:
			*d = value.([]string)
		case **string:
			*d = value.(*string)
		case *map[string]any:
			*d = value.(map[string]any)
		case *time.Time:
			*d = value.(time.Time)
		case **time.Time:
			*d = value.(*time.Time)
		}
	}
	return nil
}

func TestScanPersonNormalizesNilCollections(t *testing.T) {
	now := time.Now()
	p, err := scanPerson(scanStub{values: []any{
		uuid.New(), "First", []string(nil), "Last", "First Last",
		(*string)(nil), (*string)(nil), (*string)(nil), []string(nil), map[string]any(nil),
		now, now, (*time.Time)(nil),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.MiddleNames) != 0 || len(p.PhoneNumbers) != 0 || len(p.CustomFields) != 0 {
		t.Fatalf("nil collections were not normalized: %+v", p)
	}
}

func TestScanPersonReturnsScanError(t *testing.T) {
	want := errors.New("scan failed")
	if _, err := scanPerson(scanStub{err: want}); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}

}

func TestScanPersonPreservesValues(t *testing.T) {
	now := time.Now()
	nickname := "N"
	deleted := now.Add(time.Hour)
	p, err := scanPerson(scanStub{values: []any{
		uuid.New(), "First", []string{"M"}, "Last", "First M Last",
		&nickname, (*string)(nil), &nickname, []string{"555"}, map[string]any{"x": "y"},
		now, now, &deleted,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.MiddleNames) != 1 || len(p.PhoneNumbers) != 1 || p.CustomFields["x"] != "y" || p.DeletedAt == nil {
		t.Fatalf("values were not preserved: %+v", p)
	}
}
