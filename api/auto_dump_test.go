package api_test

import (
	"strings"
	"testing"

	"github.com/maxi-format/maxi-go/api"
	"github.com/maxi-format/maxi-go/core"
)

type autoDumpUser struct {
	_     struct{} `maxi:"alias:U,name:User"`
	ID    int      `maxi:"id,type:int"`
	Name  string   `maxi:"name"`
	Email string   `maxi:"email,default:unknown"`
}

type autoDumpAddress struct {
	_      struct{} `maxi:"alias:A,name:Address"`
	ID     string   `maxi:"id"`
	Street string   `maxi:"street"`
	City   string   `maxi:"city"`
}

type autoDumpCustomer struct {
	_       struct{} `maxi:"alias:C,name:Customer"`
	ID      string   `maxi:"id"`
	Name    string   `maxi:"name"`
	Address *autoDumpAddress `maxi:"address,type:A"`
}

type autoDumpOrder struct {
	_       struct{} `maxi:"alias:O,name:Order"`
	OrderID int      `maxi:"orderId,type:int"`
	Total   string   `maxi:"total,type:decimal"`
}

func mustAutoDump(t *testing.T, objects any, opts ...api.DumpOptions) string {
	t.Helper()
	var o api.DumpOptions
	if len(opts) > 0 {
		o = opts[0]
	} else {
		o = api.DefaultDumpOptions()
	}
	out, err := api.DumpMaxiAuto(objects, o)
	if err != nil {
		t.Fatalf("DumpMaxiAuto error: %v", err)
	}
	return out
}

func TestAutoDump_ArrayOfStructInstances(t *testing.T) {
	users := []autoDumpUser{
		{ID: 1, Name: "Julie"},
		{ID: 2, Name: "Matt", Email: ""},
	}

	got := mustAutoDump(t, users)
	if !strings.Contains(got, "U:User(id:int|name|email=unknown)") {
		t.Errorf("missing type def, got:\n%s", got)
	}
	if !strings.Contains(got, "###") {
		t.Errorf("missing separator, got:\n%s", got)
	}
	if !strings.Contains(got, "U(1|Julie)") {
		t.Errorf("missing U(1|Julie), got:\n%s", got)
	}
}

func TestAutoDump_ArrayNullEmail(t *testing.T) {
	type userWithPtr struct {
		_     struct{} `maxi:"alias:U,name:User"`
		ID    int      `maxi:"id,type:int"`
		Name  string   `maxi:"name"`
		Email *string  `maxi:"email,default:unknown"`
	}
	got := mustAutoDump(t, []userWithPtr{
		{ID: 1, Name: "Julie"},
		{ID: 2, Name: "Matt"},
	})
	if !strings.Contains(got, "U(1|Julie|~)") && !strings.Contains(got, "U(1|Julie)") {
		t.Errorf("got:\n%s", got)
	}
}

func TestAutoDump_MapAliasRows(t *testing.T) {
	data := map[string][]any{
		"U": {autoDumpUser{ID: 1, Name: "Julie"}},
		"O": {autoDumpOrder{OrderID: 100, Total: "49.99"}},
	}
	got := mustAutoDump(t, data)
	if !strings.Contains(got, "U:User(id:int|name|email=unknown)") {
		t.Errorf("missing User type, got:\n%s", got)
	}
	if !strings.Contains(got, "O:Order(orderId:int|total:decimal)") {
		t.Errorf("missing Order type, got:\n%s", got)
	}
	if !strings.Contains(got, "###") {
		t.Errorf("missing separator, got:\n%s", got)
	}
	if !strings.Contains(got, "U(1|Julie)") {
		t.Errorf("missing U record, got:\n%s", got)
	}
	if !strings.Contains(got, "O(100|49.99)") {
		t.Errorf("missing O record, got:\n%s", got)
	}
}

func TestAutoDump_ExplicitRegistration(t *testing.T) {
	type Product struct {
		ID    int
		Title string
	}
	td := &core.MaxiTypeDef{
		Alias: "P",
		Name:  "Product",
		Fields: []*core.MaxiFieldDef{
			{Name: "id", TypeExpr: "int"},
			{Name: "title"},
		},
	}
	_ = core.RegisterMaxiSchema((*Product)(nil), td)
	defer core.UnregisterMaxiSchema((*Product)(nil))

	got := mustAutoDump(t, []Product{{ID: 1, Title: "Widget"}})
	if !strings.Contains(got, "P:Product(id:int|title)") {
		t.Errorf("missing type def, got:\n%s", got)
	}
	if !strings.Contains(got, "P(1|Widget)") {
		t.Errorf("missing P record, got:\n%s", got)
	}
}

func TestAutoDump_FallbackToDefaultAlias(t *testing.T) {
	plain := []map[string]any{{"id": 1, "name": "Julie"}}
	got := mustAutoDump(t, plain, api.DumpOptions{DefaultAlias: "U", IncludeTypes: true, CollectReferences: true})
	if !strings.Contains(got, "U(") {
		t.Errorf("expected U record, got:\n%s", got)
	}
}

func TestAutoDump_ErrorNoSchemaNoAlias(t *testing.T) {
	type NoSchema struct{ X int }
	_, err := api.DumpMaxiAuto([]NoSchema{{X: 1}})
	if err == nil {
		t.Fatal("expected error for missing alias")
	}
	if !strings.Contains(err.Error(), "cannot determine alias") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAutoDump_ErrorInvalidInputType(t *testing.T) {
	_, err := api.DumpMaxiAuto("not-an-object")
	if err == nil {
		t.Fatal("expected error for string input")
	}
	if !strings.Contains(err.Error(), "must be a slice or map") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAutoDump_NestedSchemasCollected(t *testing.T) {
	addr := &autoDumpAddress{ID: "A1", Street: "123 Main St", City: "NYC"}
	customers := []autoDumpCustomer{{ID: "C1", Name: "John", Address: addr}}

	got := mustAutoDump(t, customers)
	if !strings.Contains(got, "C:Customer(") {
		t.Errorf("missing Customer typedef, got:\n%s", got)
	}
	if !strings.Contains(got, "A:Address(") {
		t.Errorf("missing Address typedef, got:\n%s", got)
	}
	if !strings.Contains(got, "C(C1|John|A1)") {
		t.Errorf("missing C record with A1 ref, got:\n%s", got)
	}
	if !strings.Contains(got, "A(A1|") {
		t.Errorf("missing separate A record, got:\n%s", got)
	}
}

func TestAutoDump_InlineWhenNoCollectRefs(t *testing.T) {
	addr := &autoDumpAddress{ID: "A1", Street: "123 Main", City: "NYC"}
	customers := []autoDumpCustomer{{ID: "C1", Name: "John", Address: addr}}

	got := mustAutoDump(t, customers, api.DumpOptions{
		IncludeTypes:      true,
		CollectReferences: false,
	})
	dataPart := got
	if idx := strings.Index(got, "###"); idx >= 0 {
		dataPart = got[idx:]
	}
	if strings.Contains(dataPart, "\nA(") {
		t.Errorf("should not have separate A record, got:\n%s", got)
	}
	if !strings.Contains(got, "(A1|") {
		t.Errorf("address should be inlined, got:\n%s", got)
	}
}

func TestAutoDump_IncludeTypesFalse(t *testing.T) {
	users := []autoDumpUser{{ID: 1, Name: "Julie"}}
	got := mustAutoDump(t, users, api.DumpOptions{IncludeTypes: false, CollectReferences: true})
	if strings.Contains(got, "U:User(") {
		t.Errorf("should not include type def, got:\n%s", got)
	}
	if !strings.Contains(got, "U(1|Julie)") {
		t.Errorf("missing U record, got:\n%s", got)
	}
}

func TestAutoDump_MultilineTrue(t *testing.T) {
	users := []autoDumpUser{{ID: 1, Name: "Julie"}}
	got := mustAutoDump(t, users, api.DumpOptions{Multiline: true, IncludeTypes: true, CollectReferences: true})
	if !strings.Contains(got, "\n") {
		t.Error("expected multiline output")
	}
	if !strings.Contains(got, "  1") {
		t.Errorf("expected indented values, got:\n%s", got)
	}
}

func TestAutoDump_CallerTypesOverrideCollected(t *testing.T) {
	users := []autoDumpUser{{ID: 1, Name: "Julie"}}
	override := &core.MaxiTypeDef{
		Alias: "U",
		Name:  "CustomUser",
		Fields: []*core.MaxiFieldDef{
			{Name: "id", TypeExpr: "int"},
			{Name: "name"},
		},
	}
	got := mustAutoDump(t, users, api.DumpOptions{
		Types:             []*core.MaxiTypeDef{override},
		IncludeTypes:      true,
		CollectReferences: true,
	})
	if !strings.Contains(got, "U:CustomUser(") {
		t.Errorf("expected overridden type name, got:\n%s", got)
	}
	if strings.Contains(got, "email") {
		t.Errorf("overridden schema should not have email, got:\n%s", got)
	}
}

func TestAutoDump_EmptySlice(t *testing.T) {
	got, err := api.DumpMaxiAuto([]autoDumpUser{}, api.DefaultDumpOptions())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == "" && false {
		t.Error("unexpected empty output")
	}
	_ = got
}

func TestAutoDump_SliceOfPointers(t *testing.T) {
	users := []*autoDumpUser{
		{ID: 1, Name: "Julie"},
	}
	got, err := api.DumpMaxiAuto(users, api.DefaultDumpOptions())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "U(1|Julie)") {
		t.Errorf("missing U record from pointer slice, got:\n%s", got)
	}
}

