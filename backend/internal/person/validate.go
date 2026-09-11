package person

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/scottfridlund/contacts/backend/internal/contactsync"
)

// Custom field policy constants (from implementation decisions).
const (
	MaxCustomFields       = 64
	MaxKeyLength          = 64
	MaxStringValueLength  = 1024
	MaxNameLength         = 255
	MaxMiddleNames        = 16
	MaxPhoneNumbers       = 10
	MaxPhoneNumberLength  = 50
	MaxEmails             = 10
	MaxEmailLength        = 254
	MaxLabelLength        = 50
	MaxAddresses          = 10
	MaxAddressFieldLength = 255
	MaxOrgFieldLength     = 255
	MaxNotesLength        = 4096
	MaxPersonLabels       = 25
	MaxPersonLabelLength  = 64
)

var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// ValidationError describes a single field-level validation failure.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationErrors is a collection of field-level failures.
type ValidationErrors []ValidationError

func (v ValidationErrors) Error() string {
	msgs := make([]string, 0, len(v))
	for _, e := range v {
		msgs = append(msgs, fmt.Sprintf("%s: %s", e.Field, e.Message))
	}
	return strings.Join(msgs, "; ")
}

// HasErrors reports whether any validation errors were recorded.
func (v ValidationErrors) HasErrors() bool { return len(v) > 0 }

// ValidateCreate validates a create payload.
func ValidateCreate(in CreateInput) ValidationErrors {
	var errs ValidationErrors
	errs = append(errs, validateRequiredName("first_name", in.FirstName)...)
	errs = append(errs, validateRequiredName("last_name", in.LastName)...)
	errs = append(errs, validateMiddleNames(in.MiddleNames)...)
	if in.Nickname != nil {
		errs = append(errs, validateOptionalShortField("nickname", *in.Nickname)...)
	}
	if in.Pronouns != nil {
		errs = append(errs, validateOptionalShortField("pronouns", *in.Pronouns)...)
	}
	if in.Birthdate != nil {
		errs = append(errs, validateBirthdate(*in.Birthdate)...)
	}
	errs = append(errs, ValidateCustomFields(in.CustomFields)...)
	errs = append(errs, validatePhoneNumbers(in.PhoneNumbers)...)
	errs = append(errs, validateEmails(in.Emails)...)
	errs = append(errs, validateAddresses(in.Addresses)...)
	if in.Organization != nil {
		errs = append(errs, validateOrganization(*in.Organization)...)
	}
	if in.Notes != nil {
		errs = append(errs, validateNotes(*in.Notes)...)
	}
	errs = append(errs, validateLabels(in.Labels)...)
	return errs
}

// ValidateUpdate validates a patch payload, only checking supplied fields.
func ValidateUpdate(in UpdateInput) ValidationErrors {
	var errs ValidationErrors
	if in.FirstNameSet {
		errs = append(errs, validateRequiredName("first_name", derefString(in.FirstName))...)
	}
	if in.LastNameSet {
		errs = append(errs, validateRequiredName("last_name", derefString(in.LastName))...)
	}
	if in.MiddleNamesSet && in.MiddleNames != nil {
		errs = append(errs, validateMiddleNames(*in.MiddleNames)...)
	}
	if in.NicknameSet && in.Nickname != nil {
		errs = append(errs, validateOptionalShortField("nickname", *in.Nickname)...)
	}
	if in.PronounsSet && in.Pronouns != nil {
		errs = append(errs, validateOptionalShortField("pronouns", *in.Pronouns)...)
	}
	if in.BirthdateSet && in.Birthdate != nil {
		errs = append(errs, validateBirthdate(*in.Birthdate)...)
	}
	if in.PhoneNumbersSet && in.PhoneNumbers != nil {
		errs = append(errs, validatePhoneNumbers(*in.PhoneNumbers)...)
	}
	if in.EmailsSet && in.Emails != nil {
		errs = append(errs, validateEmails(*in.Emails)...)
	}
	if in.AddressesSet && in.Addresses != nil {
		errs = append(errs, validateAddresses(*in.Addresses)...)
	}
	if in.OrganizationSet && in.Organization != nil {
		errs = append(errs, validateOrganization(*in.Organization)...)
	}
	if in.NotesSet && in.Notes != nil {
		errs = append(errs, validateNotes(*in.Notes)...)
	}
	if in.CustomFieldsSet {
		errs = append(errs, ValidateCustomFields(in.CustomFields)...)
	}
	if in.LabelsSet && in.Labels != nil {
		errs = append(errs, validateLabels(*in.Labels)...)
	}
	return errs
}

func validateRequiredName(field, value string) ValidationErrors {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ValidationErrors{{Field: field, Message: "is required"}}
	}
	if len(trimmed) > MaxNameLength {
		return ValidationErrors{{Field: field, Message: fmt.Sprintf("must be at most %d characters", MaxNameLength)}}
	}
	return nil
}

func validateOptionalShortField(field, value string) ValidationErrors {
	if len(value) > MaxNameLength {
		return ValidationErrors{{Field: field, Message: fmt.Sprintf("must be at most %d characters", MaxNameLength)}}
	}
	return nil
}

func validateBirthdate(value string) ValidationErrors {
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return ValidationErrors{{Field: "birthdate", Message: "must be a valid date in YYYY-MM-DD format"}}
	}
	return nil
}

func validatePhoneNumbers(numbers []contactsync.LabeledValue) ValidationErrors {
	if len(numbers) > MaxPhoneNumbers {
		return ValidationErrors{{Field: "phone_numbers", Message: fmt.Sprintf("must contain at most %d entries", MaxPhoneNumbers)}}
	}
	var errs ValidationErrors
	for i, n := range numbers {
		if strings.TrimSpace(n.Value) == "" {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("phone_numbers[%d].value", i), Message: "must not be empty"})
			continue
		}
		if len(n.Value) > MaxPhoneNumberLength {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("phone_numbers[%d].value", i), Message: fmt.Sprintf("must be at most %d characters", MaxPhoneNumberLength)})
		}
		if len(n.Label) > MaxLabelLength {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("phone_numbers[%d].label", i), Message: fmt.Sprintf("must be at most %d characters", MaxLabelLength)})
		}
	}
	return errs
}

func validateEmails(emails []contactsync.LabeledValue) ValidationErrors {
	if len(emails) > MaxEmails {
		return ValidationErrors{{Field: "emails", Message: fmt.Sprintf("must contain at most %d entries", MaxEmails)}}
	}
	var errs ValidationErrors
	for i, e := range emails {
		value := strings.TrimSpace(e.Value)
		if value == "" {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("emails[%d].value", i), Message: "must not be empty"})
			continue
		}
		if len(value) > MaxEmailLength {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("emails[%d].value", i), Message: fmt.Sprintf("must be at most %d characters", MaxEmailLength)})
		} else if !emailPattern.MatchString(value) {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("emails[%d].value", i), Message: "must be a valid email address"})
		}
		if len(e.Label) > MaxLabelLength {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("emails[%d].label", i), Message: fmt.Sprintf("must be at most %d characters", MaxLabelLength)})
		}
	}
	return errs
}

func validateAddresses(addresses []contactsync.Address) ValidationErrors {
	if len(addresses) > MaxAddresses {
		return ValidationErrors{{Field: "addresses", Message: fmt.Sprintf("must contain at most %d entries", MaxAddresses)}}
	}
	var errs ValidationErrors
	for i, a := range addresses {
		if strings.TrimSpace(a.Street) == "" && strings.TrimSpace(a.City) == "" &&
			strings.TrimSpace(a.Region) == "" && strings.TrimSpace(a.PostalCode) == "" && strings.TrimSpace(a.Country) == "" {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("addresses[%d]", i), Message: "must have at least one address field set"})
			continue
		}
		for field, value := range map[string]string{
			"label": a.Label, "street": a.Street, "city": a.City,
			"region": a.Region, "postal_code": a.PostalCode, "country": a.Country,
		} {
			max := MaxAddressFieldLength
			if field == "label" {
				max = MaxLabelLength
			}
			if len(value) > max {
				errs = append(errs, ValidationError{Field: fmt.Sprintf("addresses[%d].%s", i, field), Message: fmt.Sprintf("must be at most %d characters", max)})
			}
		}
	}
	return errs
}

func validateOrganization(org contactsync.Organization) ValidationErrors {
	var errs ValidationErrors
	for field, value := range map[string]string{"name": org.Name, "title": org.Title, "department": org.Department} {
		if len(value) > MaxOrgFieldLength {
			errs = append(errs, ValidationError{Field: "organization." + field, Message: fmt.Sprintf("must be at most %d characters", MaxOrgFieldLength)})
		}
	}
	return errs
}

func validateNotes(notes string) ValidationErrors {
	if len(notes) > MaxNotesLength {
		return ValidationErrors{{Field: "notes", Message: fmt.Sprintf("must be at most %d characters", MaxNotesLength)}}
	}
	return nil
}

func validateMiddleNames(names []string) ValidationErrors {
	if len(names) > MaxMiddleNames {
		return ValidationErrors{{Field: "middle_names", Message: fmt.Sprintf("must contain at most %d entries", MaxMiddleNames)}}
	}
	var errs ValidationErrors
	for i, n := range names {
		if strings.TrimSpace(n) == "" {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("middle_names[%d]", i), Message: "must not be empty"})
			continue
		}
		if len(n) > MaxNameLength {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("middle_names[%d]", i), Message: fmt.Sprintf("must be at most %d characters", MaxNameLength)})
		}
	}
	return errs
}

// ValidateCustomFields enforces the custom field policy: any non-empty,
// printable key up to MaxKeyLength characters (case-sensitive, no
// normalization); scalar values of type string, number, or boolean; bounded
// counts and lengths.
//
// Note: null values are rejected; to remove a field, omit it from the payload.
func ValidateCustomFields(fields map[string]any) ValidationErrors {
	if fields == nil {
		return nil
	}
	var errs ValidationErrors
	if len(fields) > MaxCustomFields {
		errs = append(errs, ValidationError{Field: "custom_fields", Message: fmt.Sprintf("must contain at most %d fields", MaxCustomFields)})
	}
	for key, value := range fields {
		field := "custom_fields." + key
		if !isValidCustomFieldKey(key) {
			errs = append(errs, ValidationError{Field: field, Message: fmt.Sprintf("key must be non-empty, at most %d characters, and contain no control characters", MaxKeyLength)})
		}
		errs = append(errs, validateCustomValue(field, value)...)
	}
	return errs
}

// isValidCustomFieldKey allows any printable key (letters, digits, spaces,
// punctuation) - matching whatever the user typed, in this app or in
// Google Contacts. Only non-empty, length, and control-character checks
// apply; case and exact formatting are preserved as-is.
func isValidCustomFieldKey(key string) bool {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" || len(key) > MaxKeyLength {
		return false
	}
	for _, r := range key {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validateCustomValue(field string, value any) ValidationErrors {
	switch v := value.(type) {
	case nil:
		return ValidationErrors{{Field: field, Message: "null values are not allowed; omit the field to remove it"}}
	case bool:
		return nil
	case float64, json.Number:
		return nil
	case string:
		if len(v) > MaxStringValueLength {
			return ValidationErrors{{Field: field, Message: fmt.Sprintf("string value must be at most %d characters", MaxStringValueLength)}}
		}
		return nil
	default:
		return ValidationErrors{{Field: field, Message: "value must be a string, number, boolean, or date string"}}
	}
}

// SanitizeCustomFieldsForSync coerces an externally-sourced custom fields map
// (e.g. parsed from a Google contact's userDefined entries) into one
// guaranteed to pass ValidateCustomFields, by dropping - never erroring on -
// anything that doesn't fit. A single oversized value or a contact with an
// unusually large number of custom fields must not block importing the rest
// of that contact's data. Keys are processed in sorted order so which
// entries survive an over-the-cap truncation is deterministic.
func SanitizeCustomFieldsForSync(raw map[string]any) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 {
		return out
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if len(out) >= MaxCustomFields {
			break
		}
		if !isValidCustomFieldKey(key) {
			continue
		}
		value := raw[key]
		if validateCustomValue("custom_fields."+key, value).HasErrors() {
			continue
		}
		out[key] = value
	}
	return out
}

// validateLabels enforces the label policy: at most MaxPersonLabels
// non-empty, printable entries up to MaxPersonLabelLength characters each -
// freeform strings (CATEGORIES-style tags), same case-sensitive/no-format
// policy as custom field keys.
func validateLabels(labels []string) ValidationErrors {
	if len(labels) > MaxPersonLabels {
		return ValidationErrors{{Field: "labels", Message: fmt.Sprintf("must contain at most %d entries", MaxPersonLabels)}}
	}
	var errs ValidationErrors
	for i, l := range labels {
		if strings.TrimSpace(l) == "" {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("labels[%d]", i), Message: "must not be empty"})
			continue
		}
		if len(l) > MaxPersonLabelLength {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("labels[%d]", i), Message: fmt.Sprintf("must be at most %d characters", MaxPersonLabelLength)})
		}
		if hasControlChar(l) {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("labels[%d]", i), Message: "must not contain control characters"})
		}
	}
	return errs
}

func hasControlChar(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// SanitizeLabelsForSync coerces externally-sourced labels (e.g. Google
// contact group names) into a set guaranteed to pass validateLabels, by
// dropping - never erroring on - anything that doesn't fit, and removing
// exact (case-sensitive) duplicates. Sorted for deterministic behavior when
// truncating to MaxPersonLabels.
func SanitizeLabelsForSync(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, l := range raw {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || len(trimmed) > MaxPersonLabelLength || hasControlChar(trimmed) {
			continue
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	if len(out) > MaxPersonLabels {
		out = out[:MaxPersonLabels]
	}
	return out
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
