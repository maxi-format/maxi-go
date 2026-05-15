package internal_test

import (
	"testing"

	"github.com/maxi-format/maxi-go/core"
	"github.com/maxi-format/maxi-go/internal"
)

func mustParseSchema(t *testing.T, schemaText string) *core.MaxiParseResult {
	t.Helper()
	result := core.NewMaxiParseResult()
	p := internal.NewSchemaParser(schemaText, result, core.DefaultParseOptions(), "")
	if err := p.Parse(); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	return result
}

func mustFailSchema(t *testing.T, schemaText string) *core.MaxiError {
	t.Helper()
	result := core.NewMaxiParseResult()
	p := internal.NewSchemaParser(schemaText, result, core.DefaultParseOptions(), "")
	err := p.Parse()
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	me, ok := err.(*core.MaxiError)
	if !ok {
		t.Fatalf("expected *MaxiError, got %T: %v", err, err)
	}
	return me
}

func TestSimpleTypeDef(t *testing.T) {
	r := mustParseSchema(t, `U:User(id:int|name|email=unknown)`)
	td := r.Schema.GetType("U")
	if td == nil {
		t.Fatal("type U not found")
	}
	if td.Name != "User" {
		t.Errorf("name: got %q, want %q", td.Name, "User")
	}
	if len(td.Fields) != 3 {
		t.Fatalf("fields: got %d, want 3", len(td.Fields))
	}
	if td.Fields[0].Name != "id" || td.Fields[0].TypeExpr != "int" {
		t.Errorf("field 0: %+v", td.Fields[0])
	}
	if td.Fields[1].Name != "name" {
		t.Errorf("field 1 name: %q", td.Fields[1].Name)
	}
	if td.Fields[2].Name != "email" || td.Fields[2].DefaultValue != "unknown" {
		t.Errorf("field 2: %+v", td.Fields[2])
	}
}

func TestAliasOnlyTypeDef(t *testing.T) {
	r := mustParseSchema(t, `P(name|age:int)`)
	td := r.Schema.GetType("P")
	if td == nil {
		t.Fatal("type P not found")
	}
	if td.Name != "" {
		t.Errorf("expected empty name, got %q", td.Name)
	}
}

func TestMultipleTypes(t *testing.T) {
	schema := `
U:User(id:int|name)
O:Order(id:int|user_id:int|total:decimal)
`
	r := mustParseSchema(t, schema)
	if !r.Schema.HasType("U") || !r.Schema.HasType("O") {
		t.Error("expected both U and O types")
	}
}

func TestMultilineTypeDef(t *testing.T) {
	schema := `U:User(
  id:int|
  name|
  email=unknown
)`
	r := mustParseSchema(t, schema)
	td := r.Schema.GetType("U")
	if td == nil || len(td.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %v", td)
	}
}

func TestRequiredAndIDConstraints(t *testing.T) {
	r := mustParseSchema(t, `U(id:int(!,id)|name(!))`)
	td := r.Schema.GetType("U")
	if td == nil {
		t.Fatal("type U not found")
	}
	id := td.Fields[0]
	if !id.IsRequired() {
		t.Error("id field should be required")
	}
	if !id.IsID() {
		t.Error("id field should be id")
	}
	name := td.Fields[1]
	if !name.IsRequired() {
		t.Error("name field should be required")
	}
}

func TestComparisonConstraints(t *testing.T) {
	r := mustParseSchema(t, `U(age:int(>=18,<=120))`)
	td := r.Schema.GetType("U")
	cs := td.Fields[0].Constraints
	if len(cs) != 2 {
		t.Fatalf("expected 2 constraints, got %d", len(cs))
	}
	if cs[0].Operator != ">=" || cs[1].Operator != "<=" {
		t.Errorf("unexpected constraints: %+v", cs)
	}
}

func TestPatternConstraint(t *testing.T) {
	r := mustParseSchema(t, `U(email:str(pattern:^[^@]+@[^@]+$))`)
	td := r.Schema.GetType("U")
	cs := td.Fields[0].Constraints
	if len(cs) != 1 || cs[0].Type != core.ConstraintPattern {
		t.Errorf("unexpected constraints: %+v", cs)
	}
}

func TestInvalidPatternConstraintErrors(t *testing.T) {
	me := mustFailSchema(t, `U(email:str(pattern:[invalid))`)
	if me.Code != core.ErrConstraintSyntax {
		t.Errorf("expected %s, got %s", core.ErrConstraintSyntax, me.Code)
	}
}

func TestMimeConstraint(t *testing.T) {
	r := mustParseSchema(t, `U(photo:bytes(mime:image/png))`)
	td := r.Schema.GetType("U")
	cs := td.Fields[0].Constraints
	if len(cs) != 1 || cs[0].Type != core.ConstraintMime {
		t.Errorf("unexpected constraints: %+v", cs)
	}
}

func TestDecimalPrecisionConstraint(t *testing.T) {
	r := mustParseSchema(t, `U(price:decimal(5.2))`)
	td := r.Schema.GetType("U")
	cs := td.Fields[0].Constraints
	if len(cs) != 1 || cs[0].Type != core.ConstraintDecimalPrecision {
		t.Errorf("unexpected constraints: %+v", cs)
	}
}

func TestExactLengthConstraint(t *testing.T) {
	r := mustParseSchema(t, `U(code:str(=5))`)
	td := r.Schema.GetType("U")
	cs := td.Fields[0].Constraints
	if len(cs) != 1 || cs[0].Type != core.ConstraintExactLength {
		t.Errorf("unexpected constraints: %+v", cs)
	}
}

func TestUnknownConstraintErrors(t *testing.T) {
	me := mustFailSchema(t, `U(x:int(unknown_thing))`)
	if me.Code != core.ErrConstraintSyntax {
		t.Errorf("expected %s, got %s", core.ErrConstraintSyntax, me.Code)
	}
}

func TestArrayTypeExpr(t *testing.T) {
	r := mustParseSchema(t, `U(tags:str[])`)
	td := r.Schema.GetType("U")
	if td.Fields[0].TypeExpr != "str[]" {
		t.Errorf("typeExpr: %q", td.Fields[0].TypeExpr)
	}
}

func TestEnumTypeExpr(t *testing.T) {
	r := mustParseSchema(t, `U(role:enum[admin,user])`)
	td := r.Schema.GetType("U")
	if td.Fields[0].TypeExpr != "enum[admin,user]" {
		t.Errorf("typeExpr: %q", td.Fields[0].TypeExpr)
	}
}

func TestAnnotation(t *testing.T) {
	r := mustParseSchema(t, `U(email:str@email)`)
	td := r.Schema.GetType("U")
	f := td.Fields[0]
	if f.TypeExpr != "str" || f.Annotation != "email" {
		t.Errorf("typeExpr=%q annotation=%q", f.TypeExpr, f.Annotation)
	}
}

func TestVersionDirective(t *testing.T) {
	r := mustParseSchema(t, "@version:1.0.0\nU(name)")
	if r.Schema.Version != "1.0.0" {
		t.Errorf("version: %q", r.Schema.Version)
	}
}

func TestUnsupportedVersionErrors(t *testing.T) {
	me := mustFailSchema(t, "@version:2.0.0\nU(name)")
	if me.Code != core.ErrUnsupportedVersion {
		t.Errorf("expected %s, got %s", core.ErrUnsupportedVersion, me.Code)
	}
}

func TestUnknownDirectiveEmitsWarning(t *testing.T) {
	r := mustParseSchema(t, "@foo:bar\nU(name)")
	if len(r.Warnings) == 0 {
		t.Error("expected a warning for unknown directive")
	}
}

func TestInheritance(t *testing.T) {
	schema := `
P:Person(id:int|name)
E:Employee<P>(department)
`
	r := mustParseSchema(t, schema)
	e := r.Schema.GetType("E")
	if e == nil {
		t.Fatal("type E not found")
	}
	if len(e.Fields) != 3 {
		t.Fatalf("Employee fields: got %d, want 3", len(e.Fields))
	}
	names := []string{e.Fields[0].Name, e.Fields[1].Name, e.Fields[2].Name}
	want := []string{"id", "name", "department"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("field %d: got %q, want %q", i, names[i], want[i])
		}
	}
}

func TestCircularInheritanceErrors(t *testing.T) {
	me := mustFailSchema(t, `
A<B>(x)
B<A>(y)
`)
	if me.Code != core.ErrCircularInheritance {
		t.Errorf("expected %s, got %s", core.ErrCircularInheritance, me.Code)
	}
}

func TestUndefinedParentErrors(t *testing.T) {
	me := mustFailSchema(t, `E<P>(department)`)
	if me.Code != core.ErrUndefinedParent {
		t.Errorf("expected %s, got %s", core.ErrUndefinedParent, me.Code)
	}
}

func TestDuplicateAliasErrors(t *testing.T) {
	me := mustFailSchema(t, "U(name)\nU(other)")
	if me.Code != core.ErrDuplicateType {
		t.Errorf("expected %s, got %s", core.ErrDuplicateType, me.Code)
	}
}

func TestTypeNameMustStartWithLetter(t *testing.T) {
	me := mustFailSchema(t, `_M:_Meta(x)`)
	if me.Code != core.ErrUnknownType {
		t.Errorf("expected %s, got %s", core.ErrUnknownType, me.Code)
	}
}

func TestUnknownFieldTypeRefErrors(t *testing.T) {
	me := mustFailSchema(t, `U(address:Addr)`)
	if me.Code != core.ErrUnknownType {
		t.Errorf("expected %s, got %s", core.ErrUnknownType, me.Code)
	}
}

func TestKnownFieldTypeRefOK(t *testing.T) {
	mustParseSchema(t, `
A:Address(street|city)
U:User(id:int|addr:A)
`)
}

func TestInvalidDefaultValueErrors(t *testing.T) {
	me := mustFailSchema(t, `U(age:int=notanumber)`)
	if me.Code != core.ErrInvalidDefaultValue {
		t.Errorf("expected %s, got %s", core.ErrInvalidDefaultValue, me.Code)
	}
}

func TestAnnotationTypeMismatchErrors(t *testing.T) {
	me := mustFailSchema(t, `U(ts:str@base64)`)
	if me.Code != core.ErrInvalidConstraintValue {
		t.Errorf("expected %s, got %s", core.ErrInvalidConstraintValue, me.Code)
	}
}

func TestBadAnnotationOnBytesErrors(t *testing.T) {
	me := mustFailSchema(t, `U(data:bytes@jpeg)`)
	if me.Code != core.ErrUnsupportedBinaryFormat {
		t.Errorf("expected %s, got %s", core.ErrUnsupportedBinaryFormat, me.Code)
	}
}

func TestResolveTypeAlias(t *testing.T) {
	r := mustParseSchema(t, `U:User(name)`)
	if got := r.Schema.ResolveTypeAlias("User"); got != "U" {
		t.Errorf("ResolveTypeAlias(User) = %q, want %q", got, "U")
	}
	if got := r.Schema.ResolveTypeAlias("U"); got != "U" {
		t.Errorf("ResolveTypeAlias(U) = %q, want %q", got, "U")
	}
}

func TestIDFieldFromConstraint(t *testing.T) {
	r := mustParseSchema(t, `U(uid:int(id)|name)`)
	td := r.Schema.GetType("U")
	if td.IDFieldName() != "uid" {
		t.Errorf("IDFieldName: %q", td.IDFieldName())
	}
}

func TestIDFieldFallbackByName(t *testing.T) {
	r := mustParseSchema(t, `U(id:int|name)`)
	td := r.Schema.GetType("U")
	if td.IDFieldName() != "id" {
		t.Errorf("IDFieldName: %q", td.IDFieldName())
	}
}

func TestNoIDField(t *testing.T) {
	r := mustParseSchema(t, `U(name|email)`)
	td := r.Schema.GetType("U")
	if td.IDField() != nil {
		t.Error("expected no id field")
	}
}

func TestEmptySchema(t *testing.T) {
	r := mustParseSchema(t, "")
	if len(r.Schema.Types()) != 0 {
		t.Error("expected no types for empty schema")
	}
}

func TestCommentLinesSkipped(t *testing.T) {
	r := mustParseSchema(t, `
# This is a comment
U(name)
# another comment
`)
	if !r.Schema.HasType("U") {
		t.Error("expected type U")
	}
}
