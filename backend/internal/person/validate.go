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
	MaxCustomFields      = 64
	MaxKeyLength         = 64
	MaxStringValueLength = 1024
	MaxNameLength        = 255
	MaxMiddleNames       = 16
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
	if in.DisplayName != nil {
		errs = append(errs, validateDisplayName(*in.DisplayName)...)
	}
	errs = append(errs, ValidateCustomFields(in.CustomFields)...)
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
	if in.DisplayNameSet && in.DisplayName != nil {
		errs = append(errs, validateDisplayName(*in.DisplayName)...)
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

func validateDisplayName(value string) ValidationErrors {
	if len(value) > MaxNameLength {
		return ValidationErrors{{Field: "display_name", Message: fmt.Sprintf("must be at most %d characters", MaxNameLength)}}
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
