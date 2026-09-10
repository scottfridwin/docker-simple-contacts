package person

import (
	"strings"
	"testing"

	"github.com/scottfridlund/contacts/backend/internal/contactsync"
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

// TestValidateUpdateNilNameTreatedAsEmpty guards derefString's nil branch:
// setting FirstNameSet/LastNameSet without an actual value must be treated
// as clearing the name to empty, which is invalid for a required field.
func TestValidateUpdateNilNameTreatedAsEmpty(t *testing.T) {
	errs := ValidateUpdate(UpdateInput{FirstNameSet: true, LastNameSet: true})
	if !errs.HasErrors() {
		t.Fatal("expected errors when first_name/last_name are set to nil")
	}
	if !strings.Contains(errs.Error(), "first_name") || !strings.Contains(errs.Error(), "last_name") {
		t.Errorf("expected first_name and last_name errors, got: %s", errs.Error())
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

	shortNotes := "Met at a conference."
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Notes: &shortNotes}); errs.HasErrors() {
		t.Errorf("expected no errors for in-bounds notes, got: %s", errs.Error())
	}
}

// TestValidateUpdateChecksEverySetField exercises ValidateUpdate with every
// *Set flag true and an invalid value, covering the update-path branches
// that mirror ValidateCreate's per-field validators.
func TestValidateUpdateChecksEverySetField(t *testing.T) {
	longName := strings.Repeat("x", MaxNameLength+1)
	badBirthdate := "not-a-date"
	badMiddleNames := []string{""}
	badPhones := []contactsync.LabeledValue{{Value: ""}}
	badEmails := []contactsync.LabeledValue{{Value: "not-an-email"}}
	badAddresses := []contactsync.Address{{}}
	badOrg := contactsync.Organization{Name: strings.Repeat("x", MaxOrgFieldLength+1)}
	badNotes := strings.Repeat("x", MaxNotesLength+1)

	errs := ValidateUpdate(UpdateInput{
		FirstNameSet: true, FirstName: &longName,
		LastNameSet: true, LastName: &longName,
		MiddleNamesSet: true, MiddleNames: &badMiddleNames,
		NicknameSet: true, Nickname: &longName,
		PronounsSet: true, Pronouns: &longName,
		BirthdateSet: true, Birthdate: &badBirthdate,
		PhoneNumbersSet: true, PhoneNumbers: &badPhones,
		EmailsSet: true, Emails: &badEmails,
		AddressesSet: true, Addresses: &badAddresses,
		OrganizationSet: true, Organization: &badOrg,
		NotesSet: true, Notes: &badNotes,
		CustomFieldsSet: true, CustomFields: map[string]any{"BadKey": "x"},
	})
	if !errs.HasErrors() {
		t.Fatal("expected errors for every invalid field")
	}
	wantFields := []string{
		"first_name", "last_name", "middle_names[0]", "nickname", "pronouns",
		"birthdate", "phone_numbers[0].value", "emails[0].value", "addresses[0]",
		"organization.name", "notes",
	}
	for _, f := range wantFields {
		found := false
		for _, e := range errs {
			if e.Field == f {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected an error for field %q, got: %s", f, errs.Error())
		}
	}
}

func TestValidatePhoneNumbers(t *testing.T) {
	tooMany := make([]contactsync.LabeledValue, MaxPhoneNumbers+1)
	for i := range tooMany {
		tooMany[i] = contactsync.LabeledValue{Value: "555-000"}
	}
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", PhoneNumbers: tooMany}); !errs.HasErrors() {
		t.Error("expected error for too many phone numbers")
	}

	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", PhoneNumbers: []contactsync.LabeledValue{{Value: ""}}}); !errs.HasErrors() {
		t.Error("expected error for empty phone number")
	}

	long := strings.Repeat("1", MaxPhoneNumberLength+1)
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", PhoneNumbers: []contactsync.LabeledValue{{Value: long}}}); !errs.HasErrors() {
		t.Error("expected error for phone number exceeding max length")
	}

	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", PhoneNumbers: []contactsync.LabeledValue{{Label: "mobile", Value: "+1-555-0100"}, {Label: "home", Value: "555-0101"}}}); errs.HasErrors() {
		t.Errorf("expected no errors for valid phone numbers, got: %s", errs.Error())
	}
	longLabel := strings.Repeat("x", MaxLabelLength+1)
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", PhoneNumbers: []contactsync.LabeledValue{{Label: longLabel, Value: "555-0100"}}}); !errs.HasErrors() {
		t.Error("expected error for an oversized phone number label")
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

func TestValidateEmails(t *testing.T) {
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Emails: []contactsync.LabeledValue{{Label: "home", Value: "not-an-email"}}}); !errs.HasErrors() {
		t.Error("expected error for invalid email address")
	}
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Emails: []contactsync.LabeledValue{{Label: "home", Value: "a@example.com"}}}); errs.HasErrors() {
		t.Errorf("expected no errors for valid email, got: %s", errs.Error())
	}
	tooMany := make([]contactsync.LabeledValue, MaxEmails+1)
	for i := range tooMany {
		tooMany[i] = contactsync.LabeledValue{Value: "a@example.com"}
	}
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Emails: tooMany}); !errs.HasErrors() {
		t.Error("expected error for too many emails")
	}
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Emails: []contactsync.LabeledValue{{Value: "  "}}}); !errs.HasErrors() {
		t.Error("expected error for a blank email value")
	}
	longValue := strings.Repeat("a", MaxEmailLength) + "@example.com"
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Emails: []contactsync.LabeledValue{{Value: longValue}}}); !errs.HasErrors() {
		t.Error("expected error for an oversized email value")
	}
	longLabel := strings.Repeat("x", MaxLabelLength+1)
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Emails: []contactsync.LabeledValue{{Label: longLabel, Value: "a@example.com"}}}); !errs.HasErrors() {
		t.Error("expected error for an oversized email label")
	}
}

func TestValidateAddresses(t *testing.T) {
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Addresses: []contactsync.Address{{Label: "home", City: "Springfield"}}}); errs.HasErrors() {
		t.Errorf("expected no errors for valid address, got: %s", errs.Error())
	}
	long := strings.Repeat("x", MaxAddressFieldLength+1)
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Addresses: []contactsync.Address{{City: long}}}); !errs.HasErrors() {
		t.Error("expected error for oversized address field")
	}
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Addresses: []contactsync.Address{{}}}); !errs.HasErrors() {
		t.Error("expected error for an address with no fields set")
	}
	longLabel := strings.Repeat("x", MaxLabelLength+1)
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Addresses: []contactsync.Address{{Label: longLabel, City: "Springfield"}}}); !errs.HasErrors() {
		t.Error("expected error for an oversized address label")
	}
	tooMany := make([]contactsync.Address, MaxAddresses+1)
	for i := range tooMany {
		tooMany[i] = contactsync.Address{City: "Springfield"}
	}
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Addresses: tooMany}); !errs.HasErrors() {
		t.Error("expected error for too many addresses")
	}
}

func TestValidateOrganization(t *testing.T) {
	long := strings.Repeat("x", MaxOrgFieldLength+1)
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Organization: &contactsync.Organization{Name: long}}); !errs.HasErrors() {
		t.Error("expected error for oversized organization field")
	}
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Organization: &contactsync.Organization{Name: "Acme", Title: "Engineer"}}); errs.HasErrors() {
		t.Errorf("expected no errors for valid organization, got: %s", errs.Error())
	}
}

func TestValidateNotes(t *testing.T) {
	long := strings.Repeat("x", MaxNotesLength+1)
	if errs := ValidateCreate(CreateInput{FirstName: "A", LastName: "B", Notes: &long}); !errs.HasErrors() {
		t.Error("expected error for oversized notes")
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

func TestCustomDateFieldValidation(t *testing.T) {
	if errs := ValidateCustomFields(map[string]any{"anniversary_date": "not-a-date"}); !errs.HasErrors() {
		t.Error("expected invalid custom date to be rejected")
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
