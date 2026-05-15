package api_test

import (
	"fmt"
	"testing"

	"github.com/maxi-format/maxi-go/api"
	"github.com/maxi-format/maxi-go/core"
	"github.com/maxi-format/maxi-go/internal"
)

func getObjectRegistry(result *core.MaxiParseResult) internal.ObjectRegistry {
	if result.ObjectRegistry == nil {
		return nil
	}
	reg, _ := result.ObjectRegistry.(internal.ObjectRegistry)
	return reg
}

func TestReferences_RegistryBuilt(t *testing.T) {
	input := `U:User(id:int|name|email)
O:Order(id:int|user:U|total:decimal)
###
U(1|Julie|julie@maxi.org)
U(2|Matt|matt@maxi.org)
O(100|1|99.99)
O(101|2|149.50)`

	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reg := getObjectRegistry(res)
	if reg == nil {
		t.Fatal("ObjectRegistry should not be nil")
	}

	if _, ok := reg["U"]; !ok {
		t.Fatal("registry should have User type")
	}
	if _, ok := reg["O"]; !ok {
		t.Fatal("registry should have Order type")
	}

	users := reg["U"]
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}

	u1 := users["1"]
	if u1["name"] != "Julie" {
		t.Errorf("user 1 name: want Julie, got %v", u1["name"])
	}
	if u1["email"] != "julie@maxi.org" {
		t.Errorf("user 1 email: want julie@maxi.org, got %v", u1["email"])
	}

	userRef := res.Records[2].Values[1]
	userRefStr := fmt.Sprintf("%v", userRef)
	if userRefStr != "1" {
		t.Errorf("order user ref: want 1, got %v (%T)", userRef, userRef)
	}
}

func TestReferences_ValidRefsNoWarnings(t *testing.T) {
	input := `U:User(id:int|name)
O:Order(id:int|user:U|total:decimal)
###
U(1|Julie)
O(100|1|99.99)`

	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range res.Warnings {
		if w.Code == core.ErrUnresolvedReference {
			t.Errorf("unexpected unresolved reference warning: %s", w.Message)
		}
	}
}

func TestReferences_UnresolvedForwardRefError(t *testing.T) {
	input := `U:User(id:int|name)
O:Order(id:int|user:U|total:decimal)
###
O(100|999|99.99)`

	opts := core.DefaultParseOptions()
	opts.AllowForwardReferences = false

	_, err := api.ParseMaxi(input, opts)
	if err == nil {
		t.Fatal("expected unresolved reference error with allowForwardReferences=false")
	}
	me, ok := err.(*core.MaxiError)
	if !ok || me.Code != core.ErrUnresolvedReference {
		t.Errorf("expected ErrUnresolvedReference, got %v", err)
	}
}

func TestReferences_UnresolvedForwardRefWarning(t *testing.T) {
	input := `U:User(id:int|name)
O:Order(id:int|user:U|total:decimal)
###
O(100|999|99.99)`

	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(res.Records))
	}
	found := false
	for _, w := range res.Warnings {
		if w.Code == core.ErrUnresolvedReference {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected UnresolvedReference warning")
	}
}

func TestReferences_ForwardReferencesResolve(t *testing.T) {
	input := `U:User(id:int|name)
O:Order(id:int|user:U|total:decimal)
###
O(100|1|99.99)
U(1|Julie)`

	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range res.Warnings {
		if w.Code == core.ErrUnresolvedReference {
			t.Error("forward reference should resolve, got warning:", w.Message)
		}
	}
	reg := getObjectRegistry(res)
	if reg == nil || reg["U"]["1"] == nil {
		t.Error("User 1 should be in registry")
	}
}

func TestReferences_InlineObjectsIndexed(t *testing.T) {
	input := `U:User(id:int|name|email)
O:Order(id:int|user:U|total:decimal)
###
O(100|(1|Julie|julie@maxi.org)|99.99)
O(101|1|149.50)`

	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	reg := getObjectRegistry(res)
	if reg == nil || reg["U"]["1"] == nil {
		t.Error("inline User 1 should be indexed in registry")
	}
	for _, w := range res.Warnings {
		if w.Code == core.ErrUnresolvedReference {
			t.Error("should not have unresolved ref for inline-indexed user")
		}
	}
}

func TestReferences_MixedInlineAndRef(t *testing.T) {
	input := `U:User(id:int|name|email)
A:Address(id:int|street|city)
O:Order(id:int|user:U|shipTo:A|total:decimal)
###
U(1|Julie|julie@maxi.org)
A(1|123 Main St|NYC)
O(100|1|1|99.99)
O(101|(2|Matt|matt@maxi.org)|(2|456 Oak Ave|LA)|149.50)`

	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range res.Warnings {
		if w.Code == core.ErrUnresolvedReference {
			t.Errorf("unexpected unresolved reference: %s", w.Message)
		}
	}
	reg := getObjectRegistry(res)
	if len(reg["U"]) != 2 {
		t.Errorf("expected 2 users, got %d", len(reg["U"]))
	}
	if len(reg["A"]) != 2 {
		t.Errorf("expected 2 addresses, got %d", len(reg["A"]))
	}
}

func TestReferences_PrimitiveFieldsNotRefs(t *testing.T) {
	input := "U:User(id:int|name|age:int|score:decimal)\n###\nU(1|Julie|25|99.5)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range res.Warnings {
		if w.Code == core.ErrUnresolvedReference {
			t.Error("primitive fields should not produce reference warnings")
		}
	}
}

func TestReferences_EnumAndMapNotRefs(t *testing.T) {
	input := "U:User(id:int|role:enum[admin,user]|meta:map)\n###\nU(1|admin|{key:value})"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range res.Warnings {
		if w.Code == core.ErrUnresolvedReference {
			t.Error("enum/map fields should not produce reference warnings")
		}
	}
}

func TestReferences_NullRefNotValidated(t *testing.T) {
	input := `U:User(id:int|name)
O:Order(id:int|user:U|total:decimal)
###
O(100|~|99.99)`

	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range res.Warnings {
		if w.Code == core.ErrUnresolvedReference {
			t.Error("null ref should not produce unresolved reference warning")
		}
	}
}

func TestReferences_AnnotationNamingInRegistry(t *testing.T) {
	input := "F:File(id:int|thumb:bytes@base64|hash:bytes@hex)\n###\nF(1|aGVsbG8=|deadbeef)"
	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	reg := getObjectRegistry(res)
	f1 := reg["F"]["1"]
	if f1 == nil {
		t.Fatal("File 1 not found in registry")
	}
	if _, ok := f1["thumb_base64"]; !ok {
		keys := fmt.Sprintf("%v", f1)
		t.Errorf("expected thumb_base64 in registry entry, keys: %s", keys)
	}
	if _, ok := f1["hash_hex"]; !ok {
		keys := fmt.Sprintf("%v", f1)
		t.Errorf("expected hash_hex in registry entry, keys: %s", keys)
	}
}

func TestReferences_ArrayReferences(t *testing.T) {
	input := `T:Tag(id:int|label)
U:User(id:int|name|tags:T[])
###
T(1|go)
T(2|maxi)
U(1|Julie|[1,2])`

	res, err := api.ParseMaxi(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tags, ok := res.Records[2].Values[2].([]any)
	if !ok {
		t.Fatalf("expected []any for tags, got %T", res.Records[2].Values[2])
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tag refs, got %d", len(tags))
	}
}
