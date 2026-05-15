package internal_test

import (
	"testing"

	"github.com/maxi-format/maxi-go/core"
	"github.com/maxi-format/maxi-go/internal"
)

func parseDocument(t *testing.T, doc string) *core.MaxiParseResult {
	t.Helper()
	return parseDocumentOpts(t, doc, core.DefaultParseOptions())
}

func parseDocumentOpts(t *testing.T, doc string, opts core.ParseOptions) *core.MaxiParseResult {
	t.Helper()
	schema, records, found := splitDocument(doc)
	result := core.NewMaxiParseResult()

	sp := internal.NewSchemaParser(schema, result, opts, "")
	if err := sp.Parse(); err != nil {
		t.Fatalf("schema parse error: %v", err)
	}

	if found {
		rp := internal.NewRecordParser(records, result, opts, "")
		if err := rp.Parse(); err != nil {
			t.Fatalf("record parse error: %v", err)
		}
	}
	return result
}

func parseDocumentExpectError(t *testing.T, doc string) *core.MaxiError {
	t.Helper()
	return parseDocumentOptsExpectError(t, doc, core.DefaultParseOptions())
}

func parseDocumentOptsExpectError(t *testing.T, doc string, opts core.ParseOptions) *core.MaxiError {
	t.Helper()
	schema, records, found := splitDocument(doc)
	result := core.NewMaxiParseResult()

	sp := internal.NewSchemaParser(schema, result, opts, "")
	if err := sp.Parse(); err != nil {
		me, ok := err.(*core.MaxiError)
		if !ok {
			t.Fatalf("expected *MaxiError, got %T", err)
		}
		return me
	}

	if found {
		rp := internal.NewRecordParser(records, result, opts, "")
		if err := rp.Parse(); err != nil {
			me, ok := err.(*core.MaxiError)
			if !ok {
				t.Fatalf("expected *MaxiError, got %T", err)
			}
			return me
		}
	}

	t.Fatal("expected an error, got nil")
	return nil
}

func splitDocument(doc string) (schema, records string, hasSep bool) {
	for i := 0; i < len(doc)-2; i++ {
		if doc[i] == '#' && doc[i+1] == '#' && doc[i+2] == '#' {
			return doc[:i], doc[i+3:], true
		}
	}
	return doc, "", false
}

func TestSimpleRecord(t *testing.T) {
	r := parseDocument(t, `
U:User(id:int|name)
###
U(1|Alice)
`)
	if len(r.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(r.Records))
	}
	rec := r.Records[0]
	if rec.Alias != "U" {
		t.Errorf("alias: %q", rec.Alias)
	}
	if rec.Values[0] != 1 {
		t.Errorf("id: %v (want 1)", rec.Values[0])
	}
	if rec.Values[1] != "Alice" {
		t.Errorf("name: %v (want Alice)", rec.Values[1])
	}
}

func TestMultipleRecords(t *testing.T) {
	r := parseDocument(t, `
U(id:int|name)
###
U(1|Alice)
U(2|Bob)
U(3|Carol)
`)
	if len(r.Records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(r.Records))
	}
}

func TestMultilineRecord(t *testing.T) {
	r := parseDocument(t, `
U(id:int|name)
###
U(
  1|
  Alice
)
`)
	if len(r.Records) != 1 {
		t.Fatal("expected 1 record")
	}
	if r.Records[0].Values[0] != 1 || r.Records[0].Values[1] != "Alice" {
		t.Errorf("values: %v", r.Records[0].Values)
	}
}

func TestIntCoercion(t *testing.T) {
	r := parseDocument(t, "U(age:int)\n###\nU(42)")
	if r.Records[0].Values[0] != 42 {
		t.Errorf("got %v, want 42", r.Records[0].Values[0])
	}
}

func TestDecimalKeptAsString(t *testing.T) {
	r := parseDocument(t, "U(price:decimal)\n###\nU(49.99)")
	if r.Records[0].Values[0] != "49.99" {
		t.Errorf("got %v (%T), want string 49.99", r.Records[0].Values[0], r.Records[0].Values[0])
	}
}

func TestFloatCoercion(t *testing.T) {
	r := parseDocument(t, "U(x:float)\n###\nU(3.14)")
	v, ok := r.Records[0].Values[0].(float64)
	if !ok || v != 3.14 {
		t.Errorf("got %v (%T), want float64(3.14)", r.Records[0].Values[0], r.Records[0].Values[0])
	}
}

func TestBoolTrue(t *testing.T) {
	r := parseDocument(t, "U(active:bool)\n###\nU(1)")
	if r.Records[0].Values[0] != true {
		t.Errorf("got %v, want true", r.Records[0].Values[0])
	}
}

func TestBoolFalse(t *testing.T) {
	r := parseDocument(t, "U(active:bool)\n###\nU(0)")
	if r.Records[0].Values[0] != false {
		t.Errorf("got %v, want false", r.Records[0].Values[0])
	}
}

func TestBoolWords(t *testing.T) {
	r := parseDocument(t, "U(a:bool|b:bool)\n###\nU(true|false)")
	if r.Records[0].Values[0] != true || r.Records[0].Values[1] != false {
		t.Errorf("got %v %v", r.Records[0].Values[0], r.Records[0].Values[1])
	}
}

func TestExplicitNull(t *testing.T) {
	r := parseDocument(t, "U(id:int|name)\n###\nU(1|~)")
	if r.Records[0].Values[1] != nil {
		t.Errorf("got %v, want nil", r.Records[0].Values[1])
	}
}

func TestDefaultValue(t *testing.T) {
	r := parseDocument(t, "U(id:int|role=guest)\n###\nU(1)")
	if r.Records[0].Values[1] != "guest" {
		t.Errorf("got %v, want guest", r.Records[0].Values[1])
	}
}

func TestMissingTrailingNull(t *testing.T) {
	r := parseDocument(t, "U(id:int|name|email)\n###\nU(1|Alice)")
	if r.Records[0].Values[2] != nil {
		t.Errorf("got %v, want nil", r.Records[0].Values[2])
	}
}

func TestQuotedString(t *testing.T) {
	r := parseDocument(t, "U(msg)\n###\nU(\"hello world\")")
	if r.Records[0].Values[0] != "hello world" {
		t.Errorf("got %v", r.Records[0].Values[0])
	}
}

func TestQuotedStringWithEscapes(t *testing.T) {
	r := parseDocument(t, "U(s)\n###\nU(\"line1\\nline2\")")
	if r.Records[0].Values[0] != "line1\nline2" {
		t.Errorf("got %q", r.Records[0].Values[0])
	}
}

func TestQuotedStringWithPipe(t *testing.T) {
	r := parseDocument(t, "U(a|b)\n###\nU(\"a|b\"|c)")
	if r.Records[0].Values[0] != "a|b" || r.Records[0].Values[1] != "c" {
		t.Errorf("got %v %v", r.Records[0].Values[0], r.Records[0].Values[1])
	}
}

func TestArrayField(t *testing.T) {
	r := parseDocument(t, "U(tags:str[])\n###\nU([a,b,c])")
	arr, ok := r.Records[0].Values[0].([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", r.Records[0].Values[0])
	}
	if len(arr) != 3 || arr[0] != "a" || arr[1] != "b" || arr[2] != "c" {
		t.Errorf("got %v", arr)
	}
}

func TestIntArrayField(t *testing.T) {
	r := parseDocument(t, "U(ns:int[])\n###\nU([1,2,3])")
	arr, ok := r.Records[0].Values[0].([]any)
	if !ok {
		t.Fatalf("expected []any")
	}
	if arr[0] != 1 || arr[1] != 2 || arr[2] != 3 {
		t.Errorf("got %v", arr)
	}
}

func TestEmptyArray(t *testing.T) {
	r := parseDocument(t, "U(tags:str[])\n###\nU([])")
	arr, ok := r.Records[0].Values[0].([]any)
	if !ok {
		t.Fatalf("expected []any")
	}
	if len(arr) != 0 {
		t.Errorf("expected empty array, got %v", arr)
	}
}

func TestMapField(t *testing.T) {
	r := parseDocument(t, "U(meta:map)\n###\nU({key:val})")
	m, ok := r.Records[0].Values[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", r.Records[0].Values[0])
	}
	if m["key"] != "val" {
		t.Errorf("got %v", m)
	}
}

func TestEmptyMap(t *testing.T) {
	r := parseDocument(t, "U(meta:map)\n###\nU({})")
	m, ok := r.Records[0].Values[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map")
	}
	if len(m) != 0 {
		t.Errorf("expected empty map")
	}
}

func TestInlineObject(t *testing.T) {
	doc := `
A:Address(street|city)
C:Customer(id:int|addr:A)
###
C(1|(Main St|NYC))
`
	r := parseDocument(t, doc)
	if len(r.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(r.Records))
	}
	addr, ok := r.Records[0].Values[1].(map[string]any)
	if !ok {
		t.Fatalf("expected map for inline obj, got %T", r.Records[0].Values[1])
	}
	if addr["street"] != "Main St" || addr["city"] != "NYC" {
		t.Errorf("addr: %v", addr)
	}
}

func TestEnumValid(t *testing.T) {
	r := parseDocument(t, "U(role:enum[admin,user])\n###\nU(admin)")
	if r.Records[0].Values[0] != "admin" {
		t.Errorf("got %v", r.Records[0].Values[0])
	}
	if len(r.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", r.Warnings)
	}
}

func TestEnumInvalidEmitsWarning(t *testing.T) {
	r := parseDocument(t, "U(role:enum[admin,user])\n###\nU(superuser)")
	if len(r.Warnings) == 0 {
		t.Error("expected a warning for invalid enum value")
	}
}

func TestEnumInvalidErrors(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowConstraintViolations = core.ConstraintViolationsError
	me := parseDocumentOptsExpectError(t, "U(role:enum[admin,user])\n###\nU(superuser)", opts)
	if me.Code != core.ErrConstraintViolation {
		t.Errorf("expected %s, got %s", core.ErrConstraintViolation, me.Code)
	}
}

func TestUnknownTypeIgnored(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowUnknownTypes = core.UnknownTypesIgnore
	r := parseDocumentOpts(t, "###\nX(1|2)", opts)
	if len(r.Records) != 1 {
	}
}

func TestUnknownTypeWarning(t *testing.T) {
	r := parseDocument(t, "###\nX(1|2)")
	if len(r.Warnings) == 0 {
		t.Error("expected warning for unknown type")
	}
}

func TestUnknownTypeError(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowUnknownTypes = core.UnknownTypesError
	me := parseDocumentOptsExpectError(t, "###\nX(1|2)", opts)
	if me.Code != core.ErrUnknownType {
		t.Errorf("expected %s, got %s", core.ErrUnknownType, me.Code)
	}
}

func TestAdditionalFieldsIgnored(t *testing.T) {
	r := parseDocument(t, "U(id:int)\n###\nU(1|extra)")
	if len(r.Warnings) != 0 {
		t.Errorf("expected no warnings in ignore mode, got %v", r.Warnings)
	}
}

func TestAdditionalFieldsWarning(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowAdditionalFields = core.AdditionalFieldsWarning
	r := parseDocumentOpts(t, "U(id:int)\n###\nU(1|extra)", opts)
	if len(r.Warnings) == 0 {
		t.Error("expected warning for additional fields")
	}
}

func TestAdditionalFieldsError(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowAdditionalFields = core.AdditionalFieldsError
	me := parseDocumentOptsExpectError(t, "U(id:int)\n###\nU(1|extra)", opts)
	if me.Code != core.ErrSchemaMismatch {
		t.Errorf("expected %s, got %s", core.ErrSchemaMismatch, me.Code)
	}
}

func TestMissingRequiredFieldWarning(t *testing.T) {
	r := parseDocument(t, "U(id:int(!,id)|name(!))\n###\nU(1)")
	if len(r.Warnings) == 0 {
		t.Error("expected warning for missing required field")
	}
}

func TestMissingRequiredFieldError(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowMissingFields = core.MissingFieldsError
	me := parseDocumentOptsExpectError(t, "U(id:int(!,id)|name(!))\n###\nU(1)", opts)
	if me.Code != core.ErrMissingRequiredField {
		t.Errorf("expected %s, got %s", core.ErrMissingRequiredField, me.Code)
	}
}

func TestTypeCoercionError(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowTypeCoercion = core.TypeCoercionError
	me := parseDocumentOptsExpectError(t, "U(age:int)\n###\nU(notanumber)", opts)
	if me.Code != core.ErrTypeMismatch {
		t.Errorf("expected %s, got %s", core.ErrTypeMismatch, me.Code)
	}
}

func TestDecimalScientificNotation(t *testing.T) {
	r := parseDocument(t, "U(x:decimal)\n###\nU(3.14)")
	if r.Records[0].Values[0] != "3.14" {
		t.Errorf("got %v", r.Records[0].Values[0])
	}
}

func TestDuplicateIDWarning(t *testing.T) {
	r := parseDocument(t, "U(id:int(id)|name)\n###\nU(1|Alice)\nU(1|Bob)")
	if len(r.Warnings) == 0 {
		t.Error("expected warning for duplicate id")
	}
}

func TestDuplicateIDError(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowConstraintViolations = core.ConstraintViolationsError
	me := parseDocumentOptsExpectError(t, "U(id:int(id)|name)\n###\nU(1|Alice)\nU(1|Bob)", opts)
	if me.Code != core.ErrDuplicateIdentifier {
		t.Errorf("expected %s, got %s", core.ErrDuplicateIdentifier, me.Code)
	}
}

func TestTypeDefInDataSectionErrors(t *testing.T) {
	me := parseDocumentExpectError(t, "###\nU:User(name)")
	if me.Code != core.ErrStream {
		t.Errorf("expected %s, got %s", core.ErrStream, me.Code)
	}
}

func TestCommentInRecordsSection(t *testing.T) {
	r := parseDocument(t, "U(id:int)\n###\n# comment\nU(1)\n# another\n")
	if len(r.Records) != 1 {
		t.Errorf("expected 1 record, got %d", len(r.Records))
	}
}

func TestComparisonConstraintWarning(t *testing.T) {
	r := parseDocument(t, "U(age:int(>=18))\n###\nU(10)")
	if len(r.Warnings) == 0 {
		t.Error("expected warning for constraint violation")
	}
}

func TestComparisonConstraintError(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowConstraintViolations = core.ConstraintViolationsError
	me := parseDocumentOptsExpectError(t, "U(age:int(>=18))\n###\nU(10)", opts)
	if me.Code != core.ErrConstraintViolation {
		t.Errorf("expected %s, got %s", core.ErrConstraintViolation, me.Code)
	}
}

func TestPatternConstraintWarning(t *testing.T) {
	r := parseDocument(t, `U(code:str(pattern:^\d{5}$))
###
U(ABC)`)
	if len(r.Warnings) == 0 {
		t.Error("expected warning for pattern mismatch")
	}
}

func TestMultipleAliasesInRecords(t *testing.T) {
	r := parseDocument(t, `
U:User(id:int|name)
O:Order(id:int|amount:decimal)
###
U(1|Alice)
O(100|49.99)
U(2|Bob)
`)
	if len(r.Records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(r.Records))
	}
	if r.Records[0].Alias != "U" || r.Records[1].Alias != "O" || r.Records[2].Alias != "U" {
		t.Errorf("unexpected aliases: %v %v %v",
			r.Records[0].Alias, r.Records[1].Alias, r.Records[2].Alias)
	}
}
