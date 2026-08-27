package person

import (
	"strings"
	"testing"
)

func TestValidateCreateRequiresNames(t *testing.T) {
	errs := ValidateCreate(CreateInput{})
	if !errs.HasErrors() {
		t.Fatal("expected validation errors for empty input")
	}
	if !strings.Contains(errs.Error(), "first_name") || !strings.Contains(errs.Error(), "last_name") {
		t.Errorf("expected first_name and last_name errors, got: %s", errs.Error())
	}
}

func TestValidateCreateValid(t *testing.T) {
	errs := ValidateCreate(CreateInput{FirstName: "Scott", LastName: "Fridlund"})
	if errs.HasErrors() {
		t.Errorf("expected no errors, got: %s", errs.Error())
	}
}

func TestCustomFieldKeyFormat(t *testing.T) {
	cases := map[string]bool{
		"blood_type": true,
		"age":        true,
		"field1":     true,
		"BloodType":  false,
		"blood-type": false,
		"_leading":   false,
		"trailing_":  false,
		"double__us": false,
	}
	for key, valid := range cases {
		errs := ValidateCustomFields(map[string]any{key: "x"})
		if valid && errs.HasErrors() {
			t.Errorf("key %q should be valid, got: %s", key, errs.Error())
		}
		if !valid && !errs.HasErrors() {
			t.Errorf("key %q should be invalid", key)
		}
	}
}

func TestCustomFieldValueTypes(t *testing.T) {
	valid := map[string]any{
		"a_string": "hello",
		"a_number": float64(42),
		"a_bool":   true,
		"a_date":   "2026-08-25",
	}
	if errs := ValidateCustomFields(valid); errs.HasErrors() {
		t.Errorf("expected valid scalar values, got: %s", errs.Error())
	}

	if errs := ValidateCustomFields(map[string]any{"nested": map[string]any{"x": 1}}); !errs.HasErrors() {
		t.Error("expected error for nested object value")
	}
	if errs := ValidateCustomFields(map[string]any{"null_val": nil}); !errs.HasErrors() {
		t.Error("expected error for null value")
	}
}

func TestCustomFieldLimits(t *testing.T) {
	tooMany := make(map[string]any, MaxCustomFields+1)
	for i := 0; i <= MaxCustomFields; i++ {
		tooMany["field_"+strings.Repeat("a", 1)+itoa(i)] = "v"
	}
	if errs := ValidateCustomFields(tooMany); !errs.HasErrors() {
		t.Error("expected error for exceeding max field count")
	}

	longString := strings.Repeat("x", MaxStringValueLength+1)
	if errs := ValidateCustomFields(map[string]any{"big": longString}); !errs.HasErrors() {
		t.Error("expected error for oversized string value")
	}
}

func TestValidateNewFields(t *testing.T) {
	long := strings.Repeat("x", MaxNameLength+1)
	longPtr := long

	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Nickname: &longPtr}); !errs.HasErrors() {
		t.Error("expected error for oversized nickname")
	}

	bad := "not-a-date"
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Birthdate: &bad}); !errs.HasErrors() {
		t.Error("expected error for invalid birthdate format")
	}

	good := "1990-01-15"
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Birthdate: &good}); errs.HasErrors() {
		t.Errorf("expected no errors for valid birthdate, got: %s", errs.Error())
	}
}

func TestValidatePhoneNumbers(t *testing.T) {
	tooMany := make([]string, MaxPhoneNumbers+1)
	for i := range tooMany {
		tooMany[i] = "555-000"
	}
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", PhoneNumbers: tooMany}); !errs.HasErrors() {
		t.Error("expected error for too many phone numbers")
	}

	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", PhoneNumbers: []string{""}}); !errs.HasErrors() {
		t.Error("expected error for empty phone number")
	}

	long := strings.Repeat("1", MaxPhoneNumberLength+1)
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", PhoneNumbers: []string{long}}); !errs.HasErrors() {
		t.Error("expected error for phone number exceeding max length")
	}

	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", PhoneNumbers: []string{"+1-555-0100", "555-0101"}}); errs.HasErrors() {
		t.Errorf("expected no errors for valid phone numbers, got: %s", errs.Error())
	}
}

func TestValidateMiddleNames(t *testing.T) {
	tooMany := make([]string, MaxMiddleNames+1)
	for i := range tooMany {
		tooMany[i] = "m"
	}
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", MiddleNames: tooMany}); !errs.HasErrors() {
		t.Error("expected error for too many middle names")
	}
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", MiddleNames: []string{""}}); !errs.HasErrors() {
		t.Error("expected error for empty middle name")
	}
}

func TestValidateUpdateFields(t *testing.T) {
	empty := ""
	if errs := ValidateUpdate(UpdateInput{FirstName: &empty, FirstNameSet: true}); !errs.HasErrors() {
		t.Error("expected error for empty first_name on update")
	}
	valid := "Ok"
	if errs := ValidateUpdate(UpdateInput{FirstName: &valid, FirstNameSet: true}); errs.HasErrors() {
		t.Errorf("unexpected errors: %s", errs.Error())
	}
}

func TestDeriveDisplayName(t *testing.T) {
	got := DeriveDisplayName("Scott", []string{"A", "B"}, "Fridlund")
	if got != "Scott A B Fridlund" {
		t.Errorf("DeriveDisplayName = %q", got)
	}
	if got := DeriveDisplayName("Scott", nil, "Fridlund"); got != "Scott Fridlund" {
		t.Errorf("DeriveDisplayName = %q", got)
	}
}

func TestIsDateString(t *testing.T) {
	if !IsDateString("2026-08-25") {
		t.Error("expected YYYY-MM-DD to be a date")
	}
	if !IsDateString("2026-08-25T10:00:00Z") {
		t.Error("expected RFC3339 to be a date")
	}
	if IsDateString("not a date") {
		t.Error("expected non-date string to be rejected")
	}
}

// itoa avoids importing strconv in the test for a trivial conversion.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
