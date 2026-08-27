package person

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
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
	MaxAddresses          = 10
	MaxAddressFieldLength = 255
)

var snakeCaseKey = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

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
	errs = append(errs, validateAddresses(in.Addresses)...)
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
	if in.AddressesSet && in.Addresses != nil {
		errs = append(errs, validateAddresses(*in.Addresses)...)
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

func validatePhoneNumbers(numbers []string) ValidationErrors {
	if len(numbers) > MaxPhoneNumbers {
		return ValidationErrors{{Field: "phone_numbers", Message: fmt.Sprintf("must contain at most %d entries", MaxPhoneNumbers)}}
	}
	var errs ValidationErrors
	for i, n := range numbers {
		if strings.TrimSpace(n) == "" {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("phone_numbers[%d]", i), Message: "must not be empty"})
			continue
		}
		if len(n) > MaxPhoneNumberLength {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("phone_numbers[%d]", i), Message: fmt.Sprintf("must be at most %d characters", MaxPhoneNumberLength)})
		}
	}
	return errs
}

func validateAddresses(addresses []Address) ValidationErrors {
	if len(addresses) > MaxAddresses {
		return ValidationErrors{{Field: "addresses", Message: fmt.Sprintf("must contain at most %d entries", MaxAddresses)}}
	}
	var errs ValidationErrors
	validTypes := map[string]bool{"home": true, "work": true, "other": true}
	for i, addr := range addresses {
		prefix := fmt.Sprintf("addresses[%d]", i)
		if !validTypes[addr.Type] {
			errs = append(errs, ValidationError{Field: prefix + ".type", Message: "must be 'home', 'work', or 'other'"})
		}
		if strings.TrimSpace(addr.Street) == "" {
			errs = append(errs, ValidationError{Field: prefix + ".street", Message: "must not be empty"})
		} else if len(addr.Street) > MaxAddressFieldLength {
			errs = append(errs, ValidationError{Field: prefix + ".street", Message: fmt.Sprintf("must be at most %d characters", MaxAddressFieldLength)})
		}
		if strings.TrimSpace(addr.City) == "" {
			errs = append(errs, ValidationError{Field: prefix + ".city", Message: "must not be empty"})
		} else if len(addr.City) > MaxAddressFieldLength {
			errs = append(errs, ValidationError{Field: prefix + ".city", Message: fmt.Sprintf("must be at most %d characters", MaxAddressFieldLength)})
		}
		if len(addr.State) > MaxAddressFieldLength {
			errs = append(errs, ValidationError{Field: prefix + ".state", Message: fmt.Sprintf("must be at most %d characters", MaxAddressFieldLength)})
		}
		if len(addr.PostalCode) > MaxAddressFieldLength {
			errs = append(errs, ValidationError{Field: prefix + ".postal_code", Message: fmt.Sprintf("must be at most %d characters", MaxAddressFieldLength)})
		}
		if len(addr.Country) > MaxAddressFieldLength {
			errs = append(errs, ValidationError{Field: prefix + ".country", Message: fmt.Sprintf("must be at most %d characters", MaxAddressFieldLength)})
		}
		if addr.Label != nil && len(*addr.Label) > MaxAddressFieldLength {
			errs = append(errs, ValidationError{Field: prefix + ".label", Message: fmt.Sprintf("must be at most %d characters", MaxAddressFieldLength)})
		}
	}
	return errs
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
