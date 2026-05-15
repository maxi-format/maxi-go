package api_test

import (
	"testing"

	"github.com/maxi-format/maxi-go/api"
	"github.com/maxi-format/maxi-go/core"
)

func TestParseMaxiSimple(t *testing.T) {
	input := `U:User(id:int|name)
###
U(1|Alice)
U(2|Bob)`

	result, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(result.Records))
	}
	if result.Records[0].Values[0] != 1 || result.Records[0].Values[1] != "Alice" {
		t.Errorf("record 0: %v", result.Records[0].Values)
	}
}

func TestParseMaxiSchemaOnly(t *testing.T) {
	input := `U:User(id:int|name)`
	result, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Schema.HasType("U") {
		t.Error("expected type U")
	}
	if len(result.Records) != 0 {
		t.Errorf("expected no records, got %d", len(result.Records))
	}
}

func TestParseMaxiRecordsOnly(t *testing.T) {
	input := `U(1|Alice)`
	result, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(result.Records))
	}
}

func TestParseMaxiCustomOptions(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowUnknownTypes = core.UnknownTypesError

	_, err := api.ParseMaxi("###\nX(1|2)", opts)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	me, ok := err.(*core.MaxiError)
	if !ok || me.Code != core.ErrUnknownType {
			t.Errorf("expected E201, got %v", err)
	}
}

func TestParseMaxiInheritance(t *testing.T) {
	input := `
P:Person(id:int|name)
E:Employee<P>(department)
###
E(1|Alice|Engineering)
`
	result, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("expected 1 record")
	}
	rec := result.Records[0]
	if len(rec.Values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(rec.Values))
	}
	if rec.Values[0] != 1 || rec.Values[1] != "Alice" || rec.Values[2] != "Engineering" {
		t.Errorf("values: %v", rec.Values)
	}
}

func TestParseMaxiRoundTripVersion(t *testing.T) {
	input := `@version:1.0.0
U(name)
###
U(Alice)`
	result, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Schema.Version != "1.0.0" {
		t.Errorf("version: %q", result.Schema.Version)
	}
}

func TestParseMaxiUnsupportedVersionError(t *testing.T) {
	_, err := api.ParseMaxi("@version:2.0.0\nU(name)")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseMaxiWarnings(t *testing.T) {
	input := `U(role:enum[admin,user])
###
U(superuser)`
	result, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning for invalid enum")
	}
}

func TestParseMaxiReferenceField(t *testing.T) {
	input := `
A:Address(id:int|city)
U:User(id:int|name|addr:A)
###
A(10|NYC)
U(1|Alice|10)
`
	result, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(result.Records))
	}
}

func TestParseMaxiUnresolvedRef(t *testing.T) {
	input := `
A:Address(id:int|city)
U:User(id:int|name|addr:A)
###
U(1|Alice|99)
`
	result, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning for unresolved reference")
	}
}

func TestParseMaxiForwardRefDisabledErrors(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowForwardReferences = false

	input := `
A:Address(id:int|city)
U:User(id:int|name|addr:A)
###
U(1|Alice|99)
`
	_, err := api.ParseMaxi(input, opts)
	if err == nil {
		t.Fatal("expected error for unresolved reference with allowForwardReferences=false")
	}
	me, ok := err.(*core.MaxiError)
	if !ok || me.Code != core.ErrUnresolvedReference {
				t.Errorf("expected E204, got %v", err)
	}
}

func TestParseMaxiMultilineRecord(t *testing.T) {
	input := `U(id:int|name)
###
U(
  42|
  "Hello World"
)`
	result, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("expected 1 record")
	}
	if result.Records[0].Values[0] != 42 || result.Records[0].Values[1] != "Hello World" {
		t.Errorf("values: %v", result.Records[0].Values)
	}
}

func TestParseMaxiEmptyInput(t *testing.T) {
	result, err := api.ParseMaxi("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Records) != 0 {
		t.Errorf("expected no records")
	}
}

func TestParseMaxiSeparatorOnly(t *testing.T) {
	result, err := api.ParseMaxi("###")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Records) != 0 {
		t.Errorf("expected no records, got %d", len(result.Records))
	}
}
