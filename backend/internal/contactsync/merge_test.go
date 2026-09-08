package contactsync

import (
	"testing"
	"time"
)

func TestResolveRecordMergesDifferentFields(t *testing.T) {
	base := Record{Fields: map[string]FieldState{}}
	local := Record{Fields: map[string]FieldState{
		"phone_numbers": {IsSet: true, Value: []string{"+1-555-0100"}, UpdatedAt: time.Unix(20, 0)},
	}}
	remote := Record{Fields: map[string]FieldState{
		"address": {IsSet: true, Value: "123 Main St", UpdatedAt: time.Unix(30, 0)},
	}}

	result := ResolveRecord(base, local, remote)
	if len(result.Conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %d", len(result.Conflicts))
	}
	if got := result.Record.Fields["phone_numbers"]; !got.IsSet {
		t.Fatalf("phone_numbers not preserved: %#v", got)
	}
	if got := result.Record.Fields["address"]; !got.IsSet {
		t.Fatalf("address not preserved: %#v", got)
	}
}

func TestResolveRecordUsesLastWriteWinsForSameField(t *testing.T) {
	base := Record{Fields: map[string]FieldState{
		"nickname": {IsSet: true, Value: "Scott", UpdatedAt: time.Unix(10, 0)},
	}}
	local := Record{Fields: map[string]FieldState{
		"nickname": {IsSet: true, Value: "Scotty", UpdatedAt: time.Unix(20, 0)},
	}}
	remote := Record{Fields: map[string]FieldState{
		"nickname": {IsSet: true, Value: "S. Fridlund", UpdatedAt: time.Unix(30, 0)},
	}}

	result := ResolveRecord(base, local, remote)
	if len(result.Conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %d", len(result.Conflicts))
	}
	if got := result.Record.Fields["nickname"]; got.Value != "S. Fridlund" {
		t.Fatalf("nickname = %#v, want remote newest value", got.Value)
	}
}

func TestResolveRecordReportsTieConflict(t *testing.T) {
	base := Record{Fields: map[string]FieldState{
		"pronouns": {IsSet: true, Value: "they/them", UpdatedAt: time.Unix(10, 0)},
	}}
	local := Record{Fields: map[string]FieldState{
		"pronouns": {IsSet: true, Value: "she/her", UpdatedAt: time.Unix(20, 0)},
	}}
	remote := Record{Fields: map[string]FieldState{
		"pronouns": {IsSet: true, Value: "he/him", UpdatedAt: time.Unix(20, 0)},
	}}

	result := ResolveRecord(base, local, remote)
	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(result.Conflicts))
	}
	if result.Conflicts[0].Field != "pronouns" {
		t.Fatalf("conflict field = %q, want pronouns", result.Conflicts[0].Field)
	}
	if got := result.Record.Fields["pronouns"]; got.Value != "she/her" {
		t.Fatalf("tie should prefer local deterministically, got %#v", got.Value)
	}
}
