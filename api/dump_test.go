package api_test

import (
	"strings"
	"testing"

	"github.com/maxi-format/maxi-go/api"
	"github.com/maxi-format/maxi-go/core"
)

var userTypes = []*core.MaxiTypeDef{
	{
		Alias: "U",
		Name:  "User",
		Fields: []*core.MaxiFieldDef{
			{Name: "id", TypeExpr: "int"},
			{Name: "name"},
			{Name: "email", DefaultValue: "unknown"},
		},
	},
}

func mustDump(t *testing.T, data any, opts api.DumpOptions) string {
	t.Helper()
	out, err := api.DumpMaxi(data, opts)
	if err != nil {
		t.Fatalf("DumpMaxi error: %v", err)
	}
	return out
}

func TestDump_ArrayWithDefaultAliasAndTypes(t *testing.T) {
	users := []map[string]any{
		{"id": 1, "name": "Julie"},
		{"id": 2, "name": "Matt", "email": nil},
	}
	got := mustDump(t, users, api.DumpOptions{
		DefaultAlias:      "U",
		Types:             userTypes,
		IncludeTypes:      true,
		CollectReferences: true,
	})
	want := strings.Join([]string{
		"U:User(id:int|name|email=unknown)",
		"###",
		"U(1|Julie)",
		"U(2|Matt|~)",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDump_SingleObjectWithExternalSchema(t *testing.T) {
	user := map[string]any{"id": 1, "name": "Julie"}
	got := mustDump(t, user, api.DumpOptions{
		DefaultAlias:      "U",
		SchemaFile:        "schemas/users.maxi",
		IncludeTypes:      false,
		CollectReferences: true,
	})
	want := strings.Join([]string{
		"@schema:schemas/users.maxi",
		"###",
		"U(1|Julie)",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDump_MapAliasRows(t *testing.T) {
	data := map[string][]map[string]any{
		"U": {
			{"id": 1, "name": "Julie"},
			{"id": 2, "name": "Matt", "email": nil},
		},
	}
	got := mustDump(t, data, api.DumpOptions{
		Types:             userTypes,
		IncludeTypes:      true,
		CollectReferences: true,
	})
	want := strings.Join([]string{
		"U:User(id:int|name|email=unknown)",
		"###",
		"U(1|Julie)",
		"U(2|Matt|~)",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDump_ErrorMissingDefaultAliasForSlice(t *testing.T) {
	_, err := api.DumpMaxi([]map[string]any{{"id": 1}})
	if err == nil {
		t.Fatal("expected error for missing DefaultAlias with slice")
	}
}

func TestDump_ErrorMissingDefaultAliasForSingleObject(t *testing.T) {
	_, err := api.DumpMaxi(map[string]any{"id": 1})
	if err == nil {
		t.Fatal("expected error for missing DefaultAlias with single object")
	}
}

func TestDump_RoundTripParseResult(t *testing.T) {
	result, err := api.ParseMaxi(strings.Join([]string{
		"U:User(id:int|name)",
		"###",
		"U(1|Julie)",
	}, "\n"))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	got := mustDump(t, result, api.DumpOptions{IncludeTypes: true, CollectReferences: true})
	want := strings.Join([]string{
		"U:User(id:int|name)",
		"###",
		"U(1|Julie)",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDump_BooleanAs1And0(t *testing.T) {
	data := []map[string]any{{"id": 1, "active": true, "deleted": false}}
	types := []*core.MaxiTypeDef{{
		Alias: "U",
		Fields: []*core.MaxiFieldDef{
			{Name: "id", TypeExpr: "int"},
			{Name: "active", TypeExpr: "bool"},
			{Name: "deleted", TypeExpr: "bool"},
		},
	}}
	got := mustDump(t, data, api.DumpOptions{DefaultAlias: "U", Types: types, IncludeTypes: true, CollectReferences: true})
	if !strings.Contains(got, "U(1|1|0)") {
		t.Errorf("expected bool as 1/0, got: %s", got)
	}
}

func TestDump_RoundTripBooleans(t *testing.T) {
	result, _ := api.ParseMaxi("T(1|0)\n")
	got := mustDump(t, result, api.DumpOptions{IncludeTypes: false, CollectReferences: true})
	if !strings.Contains(got, "T(1|0)") {
		t.Errorf("expected T(1|0), got: %s", got)
	}
}

func TestDump_BytesAsBase64(t *testing.T) {
	data := []map[string]any{{"id": 1, "avatar": []byte("hello")}}
	types := []*core.MaxiTypeDef{{
		Alias: "F",
		Fields: []*core.MaxiFieldDef{
			{Name: "id", TypeExpr: "int"},
			{Name: "avatar", TypeExpr: "bytes"},
		},
	}}
	got := mustDump(t, data, api.DumpOptions{DefaultAlias: "F", Types: types, IncludeTypes: true, CollectReferences: true})
	if !strings.Contains(got, "aGVsbG8=") {
		t.Errorf("expected base64 of 'hello', got: %s", got)
	}
}

func TestDump_BytesHexAnnotation(t *testing.T) {
	data := []map[string]any{{"id": 1, "hash": []byte{0xde, 0xad, 0xbe, 0xef}}}
	types := []*core.MaxiTypeDef{{
		Alias: "F",
		Fields: []*core.MaxiFieldDef{
			{Name: "id", TypeExpr: "int"},
			{Name: "hash", TypeExpr: "bytes", Annotation: "hex"},
		},
	}}
	got := mustDump(t, data, api.DumpOptions{DefaultAlias: "F", Types: types, IncludeTypes: true, CollectReferences: true})
	if !strings.Contains(got, "deadbeef") {
		t.Errorf("expected hex 'deadbeef', got: %s", got)
	}
}

func TestDump_InlineObjectsWhenNoCollectRefs(t *testing.T) {
	addr := map[string]any{"id": "A1", "street": "123 Main", "city": "NYC"}
	data := []map[string]any{{"id": 1, "address": addr}}

	types := []*core.MaxiTypeDef{
		{
			Alias: "U",
			Fields: []*core.MaxiFieldDef{
				{Name: "id", TypeExpr: "int"},
				{Name: "address", TypeExpr: "A"},
			},
		},
		{
			Alias: "A",
			Fields: []*core.MaxiFieldDef{
				{Name: "id"},
				{Name: "street"},
				{Name: "city"},
			},
		},
	}

	got := mustDump(t, data, api.DumpOptions{
		DefaultAlias:      "U",
		Types:             types,
		IncludeTypes:      true,
		CollectReferences: false,
	})
	if !strings.Contains(got, "(A1|123 Main|NYC)") {
		t.Errorf("expected inline object, got: %s", got)
	}
	dataPart := ""
	if idx := strings.Index(got, "###"); idx >= 0 {
		dataPart = got[idx:]
	}
	if strings.Contains(dataPart, "\nA(") {
		t.Errorf("should not have separate A record, got: %s", got)
	}
}

func TestDump_InheritedParentFields(t *testing.T) {
	data := map[string][]map[string]any{
		"E": {{"id": 1, "name": "Alice", "department": "Eng"}},
	}

	types := []*core.MaxiTypeDef{
		{
			Alias: "P",
			Name:  "Person",
			Fields: []*core.MaxiFieldDef{
				{Name: "id", TypeExpr: "int"},
				{Name: "name"},
			},
		},
		{
			Alias:   "E",
			Name:    "Employee",
			Parents: []string{"P"},
			Fields: []*core.MaxiFieldDef{
				{Name: "department"},
			},
		},
	}

	got := mustDump(t, data, api.DumpOptions{
		Types:             types,
		IncludeTypes:      true,
		CollectReferences: true,
	})
	if !strings.Contains(got, "E(1|Alice|Eng)") {
		t.Errorf("expected resolved parent fields, got: %s", got)
	}
}

func TestDump_ElementConstraintsSeparateFromArrayConstraints(t *testing.T) {
	types := []*core.MaxiTypeDef{{
		Alias: "T",
		Fields: []*core.MaxiFieldDef{
			{
				Name:     "tags",
				TypeExpr: "str[]",
				ElementConstraints: []core.ParsedConstraint{
					{Type: core.ConstraintComparison, Operator: ">=", Value: 3},
					{Type: core.ConstraintComparison, Operator: "<=", Value: 20},
				},
				Constraints: []core.ParsedConstraint{
					{Type: core.ConstraintComparison, Operator: ">=", Value: 1},
					{Type: core.ConstraintComparison, Operator: "<=", Value: 10},
				},
			},
		},
	}}

	got := mustDump(t, []map[string]any{}, api.DumpOptions{
		DefaultAlias:      "T",
		Types:             types,
		IncludeTypes:      true,
		CollectReferences: true,
	})
	if !strings.Contains(got, "tags:str(>=3,<=20)[](>=1,<=10)") {
		t.Errorf("expected separated constraints, got: %s", got)
	}
}

func TestDump_StringsNeedingQuotes(t *testing.T) {
	data := []map[string]any{
		{"id": 1, "note": "hello|world"},
		{"id": 2, "note": ""},
		{"id": 3, "note": "~"},
		{"id": 4, "note": "line1\nline2"},
	}
	types := []*core.MaxiTypeDef{{
		Alias: "N",
		Fields: []*core.MaxiFieldDef{
			{Name: "id", TypeExpr: "int"},
			{Name: "note"},
		},
	}}
	got := mustDump(t, data, api.DumpOptions{
		DefaultAlias: "N", Types: types, IncludeTypes: false, CollectReferences: true,
	})
	if !strings.Contains(got, `"hello|world"`) {
		t.Errorf("expected quoted pipe string, got: %s", got)
	}
	if !strings.Contains(got, "N(2)") && !strings.Contains(got, `N(2|"")`) {
		t.Errorf("expected N(2) or N(2|\"\"), got: %s", got)
	}
	if !strings.Contains(got, `"~"`) {
		t.Errorf("expected tilde quoted, got: %s", got)
	}
	if !strings.Contains(got, `\n`) {
		t.Errorf("expected escaped newline, got: %s", got)
	}
}

func TestDump_MultilineMode(t *testing.T) {
	types := []*core.MaxiTypeDef{{
		Alias: "U",
		Fields: []*core.MaxiFieldDef{
			{Name: "id", TypeExpr: "int"},
			{Name: "name"},
		},
	}}
	data := []map[string]any{{"id": 1, "name": "Julie"}}
	got := mustDump(t, data, api.DumpOptions{
		DefaultAlias:      "U",
		Types:             types,
		IncludeTypes:      true,
		Multiline:         true,
		CollectReferences: true,
	})
	if !strings.Contains(got, "  id:int") {
		t.Errorf("expected multiline type, got: %s", got)
	}
	if !strings.Contains(got, "  1") {
		t.Errorf("expected multiline record, got: %s", got)
	}
}

func TestDump_ReferencedAndInlineObjects(t *testing.T) {
	shippingAddress := map[string]any{
		"id":     "A1",
		"street": "123 Main St",
		"city":   "Anytown",
	}
	customerData := []map[string]any{
		{
			"id":   "C1",
			"name": "John Doe",
			"shippingAddress": shippingAddress,
			"orders": []any{
				map[string]any{"orderId": 101, "total": "49.99"},
				map[string]any{"orderId": 102, "total": "150"},
			},
		},
	}

	testTypes := []*core.MaxiTypeDef{
		{
			Alias: "C",
			Name:  "Customer",
			Fields: []*core.MaxiFieldDef{
				{Name: "id"},
				{Name: "name"},
				{Name: "shippingAddress", TypeExpr: "A"},
				{Name: "orders", TypeExpr: "O[]"},
			},
		},
		{
			Alias:  "A",
			Name:   "Address",
			Fields: []*core.MaxiFieldDef{{Name: "id"}, {Name: "street"}, {Name: "city"}},
		},
		{
			Alias:  "O",
			Name:   "Order",
			Fields: []*core.MaxiFieldDef{{Name: "orderId", TypeExpr: "int"}, {Name: "total", TypeExpr: "decimal"}},
		},
	}

	got := mustDump(t, customerData, api.DumpOptions{
		DefaultAlias:      "C",
		Types:             testTypes,
		IncludeTypes:      true,
		CollectReferences: true,
	})

	if !strings.Contains(got, "C(C1|John Doe|A1|") {
		t.Errorf("expected C record with A1 reference, got:\n%s", got)
	}
	if !strings.Contains(got, "A(A1|123 Main St|Anytown)") {
		t.Errorf("expected separate A record, got:\n%s", got)
	}
}

