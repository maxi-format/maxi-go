package api_test

import (
	"errors"
	"testing"

	"github.com/maxi-format/maxi-go/api"
	"github.com/maxi-format/maxi-go/core"
)

func TestConstraints_ConstraintViolationsWarning(t *testing.T) {
	input := "U:User(id:int|age:int(>=0,<=120))\n###\nU(1|200)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Value is kept as-is
	got := res.Records[0].Values[1]
	if got != int64(200) && got != 200 {
		t.Errorf("age value: want 200, got %v", got)
	}
	found := false
	for _, w := range res.Warnings {
		if w.Code == core.ErrConstraintViolation {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected ConstraintViolation warning")
	}
}

func TestConstraints_DuplicateIDWarning(t *testing.T) {
	input := "U:User(id:int|name)\n###\nU(1|Julie)\nU(1|Matt)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(res.Records))
	}
	found := false
	for _, w := range res.Warnings {
		if w.Code == core.ErrDuplicateIdentifier {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected DuplicateID warning")
	}
}

func TestConstraints_InvalidEnumWarning(t *testing.T) {
	input := "U:User(id:int|role:enum[admin,user,guest])\n###\nU(1|superadmin)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Records[0].Values[1] != "superadmin" {
		t.Errorf("role: want superadmin, got %v", res.Records[0].Values[1])
	}
	found := false
	for _, w := range res.Warnings {
		if w.Code == core.ErrConstraintViolation {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected ConstraintViolation warning for invalid enum")
	}
}

func TestConstraints_EmailOnIntErrors(t *testing.T) {
	_, err := api.ParseMaxi("U:User(id:int@email|name)\n###")
	if err == nil {
		t.Fatal("expected error for @email on int")
	}
	var me *core.MaxiError
	if !errors.As(err, &me) || me.Code != core.ErrInvalidConstraintValue {
		t.Errorf("expected ErrInvalidAnnotation, got %v", err)
	}
}

func TestConstraints_TimestampOnStrErrors(t *testing.T) {
	_, err := api.ParseMaxi("U:User(id:int|ts:str@timestamp)\n###")
	if err == nil {
		t.Fatal("expected error for @timestamp on str")
	}
	var me *core.MaxiError
	if !errors.As(err, &me) || me.Code != core.ErrInvalidConstraintValue {
		t.Errorf("expected ErrInvalidAnnotation, got %v", err)
	}
}

func TestConstraints_Base64OnStrErrors(t *testing.T) {
	_, err := api.ParseMaxi("U:User(id:int|data:str@base64)\n###")
	if err == nil {
		t.Fatal("expected error for @base64 on str")
	}
	var me *core.MaxiError
	if !errors.As(err, &me) || me.Code != core.ErrInvalidConstraintValue {
		t.Errorf("expected ErrInvalidAnnotation, got %v", err)
	}
}

func TestConstraints_EmailOnStrOK(t *testing.T) {
	input := "U:User(id:int|email:str@email)\n###\nU(1|julie@maxi.org)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Records[0].Values[1] != "julie@maxi.org" {
		t.Errorf("email: got %v", res.Records[0].Values[1])
	}
}

func TestConstraints_TimestampOnIntOK(t *testing.T) {
	input := "U:User(id:int|ts:int@timestamp)\n###\nU(1|1234567890)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := res.Records[0].Values[1]
	if got != int64(1234567890) && got != 1234567890 {
		t.Errorf("ts: want 1234567890, got %v", got)
	}
}

func TestConstraints_HexOnBytesOK(t *testing.T) {
	input := "F:File(id:int|data:bytes@hex)\n###\nF(1|48656c6c6f)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Records[0].Values[1] != "48656c6c6f" {
		t.Errorf("hex data: got %v", res.Records[0].Values[1])
	}
}

func TestConstraints_ConstraintViolationsError(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowConstraintViolations = core.ConstraintViolationsError

	_, err := api.ParseMaxi("U:User(id:int|age:int(>=0,<=120))\n###\nU(1|200)", opts)
	if err == nil {
		t.Fatal("expected constraint error with allowConstraintViolations=error")
	}
	var me *core.MaxiError
	if !errors.As(err, &me) || me.Code != core.ErrConstraintViolation {
		t.Errorf("expected ErrConstraintViolation, got %v", err)
	}
}

func TestConstraints_DuplicateIDError(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowConstraintViolations = core.ConstraintViolationsError

	_, err := api.ParseMaxi("U:User(id:int(id)|name)\n###\nU(1|Julie)\nU(1|Matt)", opts)
	if err == nil {
		t.Fatal("expected DuplicateID error with allowConstraintViolations=error")
	}
	var me *core.MaxiError
	if !errors.As(err, &me) || me.Code != core.ErrDuplicateIdentifier {
		t.Errorf("expected ErrDuplicateID, got %v", err)
	}
}

func TestConstraints_RequiredFieldMissingError(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowMissingFields = core.MissingFieldsError

	_, err := api.ParseMaxi("U:User(id:int(!)|name(!))\n###\nU(1)", opts)
	if err == nil {
		t.Fatal("expected missing field error")
	}
	var me *core.MaxiError
	if !errors.As(err, &me) || me.Code != core.ErrMissingRequiredField {
		t.Errorf("expected ErrMissingField, got %v", err)
	}
}

func TestConstraints_AdditionalFieldsError(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowAdditionalFields = core.AdditionalFieldsError

	_, err := api.ParseMaxi("U:User(id:int|name)\n###\nU(1|Julie|extra)", opts)
	if err == nil {
		t.Fatal("expected additional fields error")
	}
	var me *core.MaxiError
	if !errors.As(err, &me) || me.Code != core.ErrSchemaMismatch {
		t.Errorf("expected ErrSchemaMismatch, got %v", err)
	}
}

func TestConstraints_PatternPass(t *testing.T) {
	input := "U:User(id:int|name(pattern:^[a-z]+$))\n###\nU(1|julie)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Records[0].Values[1] != "julie" {
		t.Errorf("name: got %v", res.Records[0].Values[1])
	}
}

func TestConstraints_PatternViolationWarning(t *testing.T) {
	input := "U:User(id:int|name(pattern:^[a-z]+$))\n###\nU(1|JULIE)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, w := range res.Warnings {
		if w.Code == core.ErrConstraintViolation {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected pattern constraint warning")
	}
}

func TestConstraints_InheritanceCycleError(t *testing.T) {
	input := "A:Alpha<B>(x)\nB:Beta<A>(y)\n###"
	_, err := api.ParseMaxi(input)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	var me *core.MaxiError
	if !errors.As(err, &me) || me.Code != core.ErrCircularInheritance {
		t.Errorf("expected ErrInheritanceCycle, got %v", err)
	}
}
