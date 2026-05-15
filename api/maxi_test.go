package api_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/maxi-format/maxi-go/api"
	"github.com/maxi-format/maxi-go/core"
)

func TestParser_SchemaOnly(t *testing.T) {
	res, err := api.ParseMaxi("U:User(id:int|name|email)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Schema.HasType("U") {
		t.Error("expected type U")
	}
	if len(res.Records) != 0 {
		t.Errorf("expected 0 records, got %d", len(res.Records))
	}
}

func TestParser_FieldConstraintsParsed(t *testing.T) {
	input := `F:File(
  name(!)|
  key:str(id)|
  age:int(>=0,<=120)|
  username(pattern:^[a-z0-9_]+$)|
  data:bytes(mime:[image/png,image/jpg])|
  tags:str[](=3)
)
###`
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	td := res.Schema.GetType("F")
	if td == nil {
		t.Fatal("type F not found")
	}
	if len(td.Fields) != 6 {
		t.Fatalf("expected 6 fields, got %d", len(td.Fields))
	}

	name := td.Fields[0]
	if name.Name != "name" {
		t.Errorf("field[0]: want name, got %s", name.Name)
	}
	if !name.IsRequired() {
		t.Error("field name should be required")
	}

	key := td.Fields[1]
	if !key.IsID() {
		t.Error("field key should have id constraint")
	}

	age := td.Fields[2]
	if len(age.Constraints) != 2 {
		t.Errorf("age constraints: want 2, got %d", len(age.Constraints))
	}
	ops := []string{age.Constraints[0].Operator, age.Constraints[1].Operator}
	if ops[0] != ">=" || ops[1] != "<=" {
		t.Errorf("age operators: want >=,<=, got %v", ops)
	}

	username := td.Fields[3]
	if len(username.Constraints) == 0 || username.Constraints[0].Type != core.ConstraintPattern {
		t.Error("username should have pattern constraint")
	}

	data := td.Fields[4]
	if len(data.Constraints) == 0 || data.Constraints[0].Type != core.ConstraintMime {
		t.Error("data should have mime constraint")
	}
	if vals, ok := data.Constraints[0].Value.([]string); !ok || len(vals) != 2 {
		t.Errorf("mime values: want []string of len 2, got %T %v", data.Constraints[0].Value, data.Constraints[0].Value)
	}

	tags := td.Fields[5]
	if len(tags.Constraints) == 0 || tags.Constraints[0].Type != core.ConstraintExactLength {
		t.Error("tags should have exact-length constraint")
	}
}

func TestParser_SchemaImport(t *testing.T) {
	input := "@schema:users.mxs\n###\nU(1|Julie)"
	opts := core.DefaultParseOptions()
	opts.LoadSchema = func(path string) (string, error) {
		if path != "users.mxs" {
			t.Errorf("unexpected path: %s", path)
		}
		return "U:User(id:int|name)", nil
	}
	res, err := api.ParseMaxi(input, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Schema.HasType("U") {
		t.Error("expected type U from import")
	}
	if len(res.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(res.Records))
	}
	if res.Records[0].Values[1] != "Julie" {
		t.Errorf("name: want Julie, got %v", res.Records[0].Values[1])
	}
}

func TestParser_ImportedInheritanceResolvesWithinImport(t *testing.T) {
	schemas := map[string]string{
		"products.mxs": "TS:Timestamped(created_at)\nP:Product<TS>(id:int|name)",
		"users.mxs":    "TS:Timestamped(updated_at)\nU:User<TS>(id:int|email)",
	}
	input := "@schema:products.mxs\n@schema:users.mxs\n###\nP(1|Widget|2024-01-01)"
	opts := core.DefaultParseOptions()
	opts.LoadSchema = func(path string) (string, error) { return schemas[path], nil }

	res, err := api.ParseMaxi(input, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p := res.Schema.GetType("P")
	if p == nil {
		t.Fatal("type P not found")
	}
	fieldNames := make([]string, len(p.Fields))
	for i, f := range p.Fields {
		fieldNames[i] = f.Name
	}
	if strings.Join(fieldNames, ",") != "created_at,id,name" {
		t.Errorf("P fields: want created_at,id,name got %v", fieldNames)
	}

	u := res.Schema.GetType("U")
	if u == nil {
		t.Fatal("type U not found")
	}
	uNames := make([]string, len(u.Fields))
	for i, f := range u.Fields {
		uNames[i] = f.Name
	}
	if strings.Join(uNames, ",") != "updated_at,id,email" {
		t.Errorf("U fields: want updated_at,id,email got %v", uNames)
	}
}

func TestParser_CrossFileOverrideAllowed(t *testing.T) {
	input := "@schema:base.mxs\nU:User(id:int|name|role=admin)\n###\nU(1|Julie)"
	opts := core.DefaultParseOptions()
	opts.LoadSchema = func(_ string) (string, error) { return "U:User(id:int|name)", nil }

	res, err := api.ParseMaxi(input, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := res.Schema.GetType("U")
	if u == nil {
		t.Fatal("type U not found")
	}
	if len(u.Fields) != 3 {
		t.Errorf("U fields: want 3 (id,name,role), got %d", len(u.Fields))
	}
	if u.Fields[2].Name != "role" {
		t.Errorf("third field: want role, got %s", u.Fields[2].Name)
	}
}

func TestParser_SameFileDuplicateErrors(t *testing.T) {
	input := "U:User(id:int|name)\nU:User2(id:int|email)\n###"
	_, err := api.ParseMaxi(input)
	if err == nil {
		 t.Fatal("expected E102 for duplicate alias")
	}
	var me *core.MaxiError
	if !errors.As(err, &me) {
		t.Fatalf("expected *MaxiError, got %T", err)
	}
	if me.Code != core.ErrDuplicateType {
		t.Errorf("expected ErrDuplicateType, got %s", me.Code)
	}
}

func TestParser_ImportedFileDuplicateErrors(t *testing.T) {
	input := "@schema:bad.mxs\n###"
	opts := core.DefaultParseOptions()
	opts.LoadSchema = func(_ string) (string, error) {
		return "U:User(id:int|name)\nU:User2(id:int|email)", nil
	}
	_, err := api.ParseMaxi(input, opts)
	if err == nil {
		 t.Fatal("expected E102 for duplicate alias in import")
	}
	var me *core.MaxiError
	if !errors.As(err, &me) || me.Code != core.ErrDuplicateType {
		t.Errorf("expected ErrDuplicateType, got %v", err)
	}
}

func TestParser_TypeCoercionWarning(t *testing.T) {
	input := "U:User(id:int|name)\n###\nU(hello|Julie)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Records[0].Values[0] != "hello" {
		t.Errorf("value should remain as string, got %v", res.Records[0].Values[0])
	}
	found := false
	for _, w := range res.Warnings {
		if w.Code == core.ErrTypeMismatch {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected TypeMismatch warning")
	}
}

func TestParser_MapIntShorthand(t *testing.T) {
	input := "S:Scores(id:int|data:map<int>)\n###\nS(1|{math:95,english:87})"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	td := res.Schema.GetType("S")
	if td == nil {
		t.Fatal("type S not found")
	}
	if td.Fields[1].TypeExpr != "map<int>" {
		t.Errorf("typeExpr: want map<int>, got %s", td.Fields[1].TypeExpr)
	}
	m, ok := res.Records[0].Values[1].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", res.Records[0].Values[1])
	}
	// Values should be coerced to int
	if m["math"] != int64(95) && m["math"] != 95 {
		t.Errorf("math: want 95, got %v (%T)", m["math"], m["math"])
	}
}

func TestParser_CircularImports(t *testing.T) {
	schemas := map[string]string{
		"a.mxs": "@schema:b.mxs\nU:User(id:int|name)",
		"b.mxs": "@schema:a.mxs\nO:Order(id:int|total:decimal)",
	}
	input := "@schema:a.mxs\n###\nU(1|Julie)"
	opts := core.DefaultParseOptions()
	opts.LoadSchema = func(path string) (string, error) { return schemas[path], nil }

	res, err := api.ParseMaxi(input, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Schema.HasType("U") {
		t.Error("expected type U")
	}
	if !res.Schema.HasType("O") {
		t.Error("expected type O")
	}
}

func TestParser_VersionDirective(t *testing.T) {
	input := "@version:1.0.0\nU:User(id:int)\n###\nU(1)"
	_, err := api.ParseMaxi(input)
	if err != nil {
		t.Errorf("@version:1.0.0 should be accepted, got: %v", err)
	}

	_, err = api.ParseMaxi("@version:2.0.0\nU:User(id:int)\n###\nU(1)")
	if err == nil {
		t.Error("expected error for unsupported version")
	}
	var me *core.MaxiError
	if !errors.As(err, &me) || me.Code != core.ErrUnsupportedVersion {
		t.Errorf("expected ErrUnsupportedVersion, got %v", err)
	}
}

func TestParser_Inheritance(t *testing.T) {
	input := "P:Person(id:int|name)\nE:Employee<P>(department)\n###\nE(1|Alice|Eng)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := res.Schema.GetType("E")
	names := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		names[i] = f.Name
	}
	if names[0] != "id" || names[1] != "name" || names[2] != "department" {
		t.Errorf("Employee fields: got %v", names)
	}
	if res.Records[0].Values[0] != int64(1) && res.Records[0].Values[0] != 1 {
		t.Errorf("id: want 1, got %v", res.Records[0].Values[0])
	}
}

func TestParser_MultilineTypeDef(t *testing.T) {
	input := `U:User(
  id:int|
  name|
  email
)
###
U(1|Julie|julie@test.com)`
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Schema.GetType("U").Fields) != 3 {
		t.Errorf("expected 3 fields")
	}
	if res.Records[0].Values[2] != "julie@test.com" {
		t.Errorf("email: got %v", res.Records[0].Values[2])
	}
}

func TestParser_DefaultValues(t *testing.T) {
	input := "U:User(id:int|name|role=user)\n###\nU(1|Julie)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := res.Schema.GetType("U").Fields[2]
	if f.DefaultValue != "user" {
		t.Errorf("default: want user, got %v", f.DefaultValue)
	}
	if res.Records[0].Values[2] != "user" {
		t.Errorf("record role: want user, got %v", res.Records[0].Values[2])
	}
}

func TestParser_NullValues(t *testing.T) {
	input := "U:User(id:int|name|email)\n###\nU(1|Julie|~)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Records[0].Values[2] != nil {
		t.Errorf("expected nil for ~, got %v", res.Records[0].Values[2])
	}
}

func TestParser_QuotedStrings(t *testing.T) {
	input := `U:User(id:int|name)
###
U(1|"Julie|Smith")`
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Records[0].Values[1] != "Julie|Smith" {
		t.Errorf("quoted name: got %v", res.Records[0].Values[1])
	}
}

func TestParser_ArrayField(t *testing.T) {
	input := "T:Tag(id:int|labels:str[])\n###\nT(1|[foo,bar,baz])"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	vals, ok := res.Records[0].Values[1].([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", res.Records[0].Values[1])
	}
	if len(vals) != 3 || vals[0] != "foo" {
		t.Errorf("array values: %v", vals)
	}
}

func TestParser_EnumField(t *testing.T) {
	input := "U:User(id:int|role:enum[admin,user,guest])\n###\nU(1|admin)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Records[0].Values[1] != "admin" {
		t.Errorf("enum value: got %v", res.Records[0].Values[1])
	}
}

func TestParser_InlineObject(t *testing.T) {
	input := "A:Address(id:int|city)\nU:User(id:int|name|addr:A)\n###\nU(1|Julie|(1|NYC))"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	addr, ok := res.Records[0].Values[2].(map[string]any)
	if !ok {
		t.Fatalf("expected inline map, got %T", res.Records[0].Values[2])
	}
	if addr["city"] != "NYC" {
		t.Errorf("city: got %v", addr["city"])
	}
}

func TestParser_BoolField(t *testing.T) {
	input := "U:User(id:int|active:bool)\n###\nU(1|1)\nU(2|0)\nU(3|true)\nU(4|false)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Records[0].Values[1] != true {
		t.Errorf("1 → true: got %v", res.Records[0].Values[1])
	}
	if res.Records[1].Values[1] != false {
		t.Errorf("0 → false: got %v", res.Records[1].Values[1])
	}
}

func TestParser_DecimalField(t *testing.T) {
	input := "P:Price(id:int|amount:decimal)\n###\nP(1|19.99)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Records[0].Values[1] != "19.99" {
		t.Errorf("decimal: got %v (%T)", res.Records[0].Values[1], res.Records[0].Values[1])
	}
}

func TestParser_UnknownTypeWarning(t *testing.T) {
	res, err := api.ParseMaxi("X(1|Julie)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, w := range res.Warnings {
		if w.Code == core.ErrUnknownType {
			found = true
		}
	}
	if !found {
		t.Error("expected unknown-type warning")
	}
}

func TestParser_UnknownTypeError(t *testing.T) {
	opts := core.DefaultParseOptions()
	opts.AllowUnknownTypes = core.UnknownTypesError

	_, err := api.ParseMaxi("###\nX(1|Julie)", opts)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}
