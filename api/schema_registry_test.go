package api_test

import (
	"testing"

	"github.com/maxi-format/maxi-go/core"
)

type tagUser struct {
	_ struct{} `maxi:"alias:U,name:User"`
	ID    int    `maxi:"id,type:int,id"`
	Name  string `maxi:"name"`
	Email string `maxi:"email,default:unknown"`
}

type tagOrder struct {
	ID    int
	Total float64
}

var tagOrderSchema = &core.MaxiTypeDef{
	Alias: "O",
	Name:  "Order",
	Fields: []*core.MaxiFieldDef{
		{Name: "id", TypeExpr: "int"},
		{Name: "total", TypeExpr: "decimal"},
	},
}

func TestRegistry_GetSchemaFromStructTags(t *testing.T) {
	schema := core.GetMaxiSchema(tagUser{})
	if schema == nil {
		t.Fatal("expected schema from struct tags, got nil")
	}
	if schema.Alias != "U" {
		t.Errorf("alias: want U, got %s", schema.Alias)
	}
	if schema.Name != "User" {
		t.Errorf("name: want User, got %s", schema.Name)
	}
	if len(schema.Fields) != 3 {
		t.Errorf("fields: want 3, got %d", len(schema.Fields))
	}
}

func TestRegistry_GetSchemaFromPointerToTaggedStruct(t *testing.T) {
	u := &tagUser{}
	schema := core.GetMaxiSchema(u)
	if schema == nil {
		t.Fatal("expected schema via pointer, got nil")
	}
	if schema.Alias != "U" {
		t.Errorf("alias: want U, got %s", schema.Alias)
	}
}

func TestRegistry_FieldTypeExprDerivedFromTag(t *testing.T) {
	schema := core.GetMaxiSchema(tagUser{})
	if schema == nil {
		t.Fatal("nil schema")
	}
	idField := schema.Fields[0]
	if idField.TypeExpr != "int" {
		t.Errorf("id typeExpr: want int, got %q", idField.TypeExpr)
	}
}

func TestRegistry_FieldDefaultValueDerivedFromTag(t *testing.T) {
	schema := core.GetMaxiSchema(tagUser{})
	if schema == nil {
		t.Fatal("nil schema")
	}
	emailField := schema.Fields[2]
	if emailField.Name != "email" {
		t.Errorf("expected email field, got %q", emailField.Name)
	}
	if emailField.DefaultValue != "unknown" {
		t.Errorf("defaultValue: want unknown, got %v", emailField.DefaultValue)
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	if err := core.RegisterMaxiSchema((*tagOrder)(nil), tagOrderSchema); err != nil {
		t.Fatalf("RegisterMaxiSchema: %v", err)
	}
	defer core.UnregisterMaxiSchema((*tagOrder)(nil))

	schema := core.GetMaxiSchema(tagOrder{})
	if schema == nil {
		t.Fatal("expected registered schema, got nil")
	}
	if schema.Alias != "O" {
		t.Errorf("alias: want O, got %s", schema.Alias)
	}
	if len(schema.Fields) != 2 {
		t.Errorf("fields: want 2, got %d", len(schema.Fields))
	}
}

func TestRegistry_RegisterFromInstance(t *testing.T) {
	if err := core.RegisterMaxiSchema((*tagOrder)(nil), tagOrderSchema); err != nil {
		t.Fatalf("RegisterMaxiSchema: %v", err)
	}
	defer core.UnregisterMaxiSchema((*tagOrder)(nil))

	schema := core.GetMaxiSchema(&tagOrder{ID: 1, Total: 9.99})
	if schema == nil {
		t.Fatal("expected schema from instance, got nil")
	}
	if schema.Alias != "O" {
		t.Errorf("alias: want O, got %s", schema.Alias)
	}
}

func TestRegistry_StructTagsTakePriorityOverExplicit(t *testing.T) {
	conflict := &core.MaxiTypeDef{Alias: "WRONG", Fields: nil}
	if err := core.RegisterMaxiSchema((*tagUser)(nil), conflict); err != nil {
		t.Fatalf("RegisterMaxiSchema: %v", err)
	}
	defer core.UnregisterMaxiSchema((*tagUser)(nil))

	schema := core.GetMaxiSchema(tagUser{})
	if schema == nil {
		t.Fatal("nil schema")
	}
	if schema.Alias != "U" {
		t.Errorf("struct tags should win over explicit registry; got alias %s", schema.Alias)
	}
}

func TestRegistry_Unregister(t *testing.T) {
	type Temp struct {
		_ struct{} `maxi:"alias:T"`
		V string   `maxi:"v"`
	}

	schema := core.GetMaxiSchema(Temp{})
	if schema == nil {
		t.Fatal("expected schema from tags before unregister")
	}

	core.UnregisterMaxiSchema((*Temp)(nil))

	schema2 := core.GetMaxiSchema(Temp{})
	if schema2 == nil {
		t.Fatal("expected schema still re-derivable after unregister")
	}
}

func TestRegistry_UnregisterExplicit(t *testing.T) {
	type Temp2 struct{ ID int }
	td := &core.MaxiTypeDef{Alias: "T2", Fields: []*core.MaxiFieldDef{{Name: "id"}}}

	if err := core.RegisterMaxiSchema((*Temp2)(nil), td); err != nil {
		t.Fatal(err)
	}
	if core.GetMaxiSchema(Temp2{}) == nil {
		t.Fatal("expected schema after register")
	}

	core.UnregisterMaxiSchema((*Temp2)(nil))
	if core.GetMaxiSchema(Temp2{}) != nil {
		t.Error("expected nil after unregister of explicit-only schema")
	}
}

func TestRegistry_NilReturnsNil(t *testing.T) {
	if core.GetMaxiSchema(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestRegistry_UnregisteredReturnsNil(t *testing.T) {
	type Unknown struct{ X int }
	if core.GetMaxiSchema(Unknown{}) != nil {
		t.Error("expected nil for unregistered type without tags")
	}
}

func TestRegistry_RegisterNilCls(t *testing.T) {
	if err := core.RegisterMaxiSchema(nil, tagOrderSchema); err == nil {
		t.Error("expected error for nil cls")
	}
}

func TestRegistry_RegisterNilSchema(t *testing.T) {
	type T struct{}
	if err := core.RegisterMaxiSchema((*T)(nil), nil); err == nil {
		t.Error("expected error for nil schema")
	}
}

func TestRegistry_RegisterMissingAlias(t *testing.T) {
	type T struct{}
	if err := core.RegisterMaxiSchema((*T)(nil), &core.MaxiTypeDef{}); err == nil {
		t.Error("expected error for empty alias")
	}
}

func TestRegistry_OverwriteRegistration(t *testing.T) {
	type Product struct{ ID int }
	td1 := &core.MaxiTypeDef{Alias: "P1", Fields: []*core.MaxiFieldDef{{Name: "id"}}}
	td2 := &core.MaxiTypeDef{Alias: "P2", Fields: []*core.MaxiFieldDef{{Name: "id"}, {Name: "name"}}}

	_ = core.RegisterMaxiSchema((*Product)(nil), td1)
	_ = core.RegisterMaxiSchema((*Product)(nil), td2)
	defer core.UnregisterMaxiSchema((*Product)(nil))

	schema := core.GetMaxiSchema(Product{})
	if schema == nil {
		t.Fatal("nil schema")
	}
	if schema.Alias != "P2" {
		t.Errorf("want P2, got %s", schema.Alias)
	}
	if len(schema.Fields) != 2 {
		t.Errorf("want 2 fields, got %d", len(schema.Fields))
	}
}

func TestRegistry_FieldTagIDConstraint(t *testing.T) {
	type WithID struct {
		_  struct{} `maxi:"alias:W"`
		ID int      `maxi:"id,type:int,id"`
	}
	schema := core.GetMaxiSchema(WithID{})
	if schema == nil {
		t.Fatal("nil schema")
	}
	idField := schema.Fields[0]
	if !idField.IsID() {
		t.Error("expected id field to have id constraint")
	}
}

func TestRegistry_FieldTagRequiredConstraint(t *testing.T) {
	type WithReq struct {
		_    struct{} `maxi:"alias:R"`
		Name string   `maxi:"name,required"`
	}
	schema := core.GetMaxiSchema(WithReq{})
	if schema == nil {
		t.Fatal("nil schema")
	}
	nameField := schema.Fields[0]
	if !nameField.IsRequired() {
		t.Error("expected name field to have required constraint")
	}
}

func TestRegistry_FieldTagSkip(t *testing.T) {
	type WithSkip struct {
		_       struct{} `maxi:"alias:S"`
		Name    string   `maxi:"name"`
		Ignored string   `maxi:"-"`
	}
	schema := core.GetMaxiSchema(WithSkip{})
	if schema == nil {
		t.Fatal("nil schema")
	}
	if len(schema.Fields) != 1 {
		t.Errorf("expected 1 field (skipped ignored), got %d", len(schema.Fields))
	}
	if schema.Fields[0].Name != "name" {
		t.Errorf("expected name field, got %s", schema.Fields[0].Name)
	}
}

func TestRegistry_FieldTagAnnotation(t *testing.T) {
	type WithAnn struct {
		_     struct{} `maxi:"alias:A"`
		Photo []byte   `maxi:"photo,type:bytes,ann:base64"`
	}
	schema := core.GetMaxiSchema(WithAnn{})
	if schema == nil {
		t.Fatal("nil schema")
	}
	f := schema.Fields[0]
	if f.TypeExpr != "bytes" {
		t.Errorf("typeExpr: want bytes, got %s", f.TypeExpr)
	}
	if f.Annotation != "base64" {
		t.Errorf("annotation: want base64, got %s", f.Annotation)
	}
}

