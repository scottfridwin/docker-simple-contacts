package person

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

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
)

var snakeCaseKey = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
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

// ValidateCustomFields enforces the custom field policy: snake_case keys; scalar
// values of type string, number, boolean, or date; bounded counts and lengths.
//
// Note: JSON has no native date type. Date values are represented as strings in
// RFC 3339 or YYYY-MM-DD form and validated as such. Null values are rejected;
// to remove a field, omit it from the payload.
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
		if len(key) > MaxKeyLength {
			errs = append(errs, ValidationError{Field: field, Message: fmt.Sprintf("key must be at most %d characters", MaxKeyLength)})
		}
		if !snakeCaseKey.MatchString(key) {
			errs = append(errs, ValidationError{Field: field, Message: "key must be lowercase snake_case"})
		}
		errs = append(errs, validateCustomValue(field, value)...)
	}
	return errs
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
		if strings.HasSuffix(field, "_date") && !IsDateString(v) {
			return ValidationErrors{{Field: field, Message: "date value must be YYYY-MM-DD or RFC 3339"}}
		}
		return nil
	default:
		return ValidationErrors{{Field: field, Message: "value must be a string, number, boolean, or date string"}}
	}
}

// IsDateString reports whether a value looks like a supported date representation.
// Provided as a helper for clients; storage treats dates as strings.
func IsDateString(s string) bool {
	if _, err := time.Parse("2006-01-02", s); err == nil {
		return true
	}
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
