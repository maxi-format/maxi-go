package api_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/maxi-format/maxi-go/api"
	"github.com/maxi-format/maxi-go/core"
)

type hydrateUser struct {
	_     struct{} `maxi:"alias:U,name:User"`
	ID    int      `maxi:"id,type:int,id"`
	Name  string   `maxi:"name"`
	Email string   `maxi:"email,default:unknown"`
}

type hydrateOrder struct {
	_      struct{} `maxi:"alias:O,name:Order"`
	ID     int      `maxi:"id,type:int,id"`
	UserID any      `maxi:"userId,type:U"`
	Total  string   `maxi:"total,type:decimal"`
}

func makeMaxiInput(schemaLines, recordLines []string) string {
	return strings.Join(append(schemaLines, append([]string{"###"}, recordLines...)...), "\n")
}

func TestHydrate_BasicHydration(t *testing.T) {
	input := makeMaxiInput(
		[]string{"U:User(id:int|name|email)"},
		[]string{"U(1|Julie|julie@example.com)", "U(2|Matt|matt@example.com)"},
	)

	res, err := api.ParseMaxiAs(input, map[string]reflect.Type{
		"U": reflect.TypeOf(hydrateUser{}),
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	users := res.Objects["U"]
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}

	u0 := users[0].(*hydrateUser)
	if u0.ID != 1 {
		t.Errorf("id: want 1, got %d", u0.ID)
	}
	if u0.Name != "Julie" {
		t.Errorf("name: want Julie, got %s", u0.Name)
	}
	if u0.Email != "julie@example.com" {
		t.Errorf("email: want julie@example.com, got %s", u0.Email)
	}

	u1 := users[1].(*hydrateUser)
	if u1.ID != 2 {
		t.Errorf("id: want 2, got %d", u1.ID)
	}
}

func TestHydrate_ResultHasSchemaAndWarnings(t *testing.T) {
	input := makeMaxiInput([]string{"U:User(id:int|name)"}, []string{"U(1|Julie)"})
	res, err := api.ParseMaxiAs(input, map[string]reflect.Type{"U": reflect.TypeOf(hydrateUser{})})
	if err != nil {
		t.Fatal(err)
	}
	if res.Schema == nil {
		t.Error("expected schema, got nil")
	}
	if res.Warnings == nil {
		t.Error("expected warnings slice, got nil")
	}
}

func TestHydrate_OnlyClassMapAliasesHydrated(t *testing.T) {
	input := makeMaxiInput(
		[]string{"U:User(id:int|name)", "O:Order(id:int|userId:int|total:decimal)"},
		[]string{"U(1|Julie)", "O(100|1|49.99)"},
	)
	res, err := api.ParseMaxiAs(input, map[string]reflect.Type{"U": reflect.TypeOf(hydrateUser{})})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Objects["U"]) != 1 {
		t.Errorf("expected 1 user, got %d", len(res.Objects["U"]))
	}
	if _, ok := res.Objects["O"]; ok {
		t.Error("O should not be hydrated when not in classMap")
	}
}

func TestHydrate_CrossReferenceResolution(t *testing.T) {
	input := makeMaxiInput(
		[]string{"U:User(id:int|name)", "O:Order(id:int|userId:U|total:decimal)"},
		[]string{"U(1|Julie)", "O(100|1|49.99)"},
	)
	res, err := api.ParseMaxiAs(input, map[string]reflect.Type{
		"U": reflect.TypeOf(hydrateUser{}),
		"O": reflect.TypeOf(hydrateOrder{}),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Objects["O"]) != 1 {
		t.Fatalf("expected 1 order, got %d", len(res.Objects["O"]))
	}
	order := res.Objects["O"][0].(*hydrateOrder)

	// userId should be resolved to a *hydrateUser
	user, ok := order.UserID.(*hydrateUser)
	if !ok {
		t.Fatalf("userID should be *hydrateUser after resolution, got %T", order.UserID)
	}
	if user.ID != 1 {
		t.Errorf("user id: want 1, got %d", user.ID)
	}
	if user.Name != "Julie" {
		t.Errorf("user name: want Julie, got %s", user.Name)
	}
}

func TestHydrate_UnresolvedReferenceStaysScalar(t *testing.T) {
	input := makeMaxiInput(
		[]string{"U:User(id:int|name)", "O:Order(id:int|userId:U|total:decimal)"},
		[]string{"O(100|999|49.99)"},
	)
	res, err := api.ParseMaxiAs(input, map[string]reflect.Type{
		"U": reflect.TypeOf(hydrateUser{}),
		"O": reflect.TypeOf(hydrateOrder{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	order := res.Objects["O"][0].(*hydrateOrder)
	if order.UserID == nil {
		t.Error("userID should remain as scalar, not nil")
	}
	idVal := fmt.Sprintf("%v", order.UserID)
	if idVal != "999" {
		t.Errorf("unresolved userID should be 999, got %v", order.UserID)
	}
}

func TestHydrate_ForwardReferencesResolve(t *testing.T) {
	input := makeMaxiInput(
		[]string{"U:User(id:int|name)", "O:Order(id:int|userId:U|total:decimal)"},
		[]string{"O(100|1|49.99)", "U(1|Julie)"}, // Order before User
	)
	res, err := api.ParseMaxiAs(input, map[string]reflect.Type{
		"U": reflect.TypeOf(hydrateUser{}),
		"O": reflect.TypeOf(hydrateOrder{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	order := res.Objects["O"][0].(*hydrateOrder)
	user, ok := order.UserID.(*hydrateUser)
	if !ok {
		t.Fatalf("forward ref should resolve to *hydrateUser, got %T", order.UserID)
	}
	if user.Name != "Julie" {
		t.Errorf("user name: want Julie, got %s", user.Name)
	}
}

func TestHydrate_NullValuesMappedToZero(t *testing.T) {
	input := makeMaxiInput(
		[]string{"U:User(id:int|name|email)"},
		[]string{"U(1|Julie|~)"},
	)
	res, err := api.ParseMaxiAs(input, map[string]reflect.Type{"U": reflect.TypeOf(hydrateUser{})})
	if err != nil {
		t.Fatal(err)
	}
	u := res.Objects["U"][0].(*hydrateUser)
	if u.Email != "" {
		t.Errorf("null email should be zero string, got %q", u.Email)
	}
}

func TestHydrate_DefaultValueFieldsFilled(t *testing.T) {
	input := makeMaxiInput(
		[]string{"U:User(id:int|name|email=unknown)"},
		[]string{"U(1|Julie)"},
	)
	res, err := api.ParseMaxiAs(input, map[string]reflect.Type{"U": reflect.TypeOf(hydrateUser{})})
	if err != nil {
		t.Fatal(err)
	}
	u := res.Objects["U"][0].(*hydrateUser)
	if u.Email != "unknown" {
		t.Errorf("default email: want unknown, got %q", u.Email)
	}
}

func TestHydrate_NilClassMap(t *testing.T) {
	_, err := api.ParseMaxiAs("U(1|Julie)", nil)
	if err == nil {
		t.Fatal("expected error for nil classMap")
	}
	if !strings.Contains(err.Error(), "classMap") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHydrate_EmptyRecords(t *testing.T) {
	input := "U:User(id:int|name)\n###\n"
	res, err := api.ParseMaxiAs(input, map[string]reflect.Type{"U": reflect.TypeOf(hydrateUser{})})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(res.Objects["U"]); n != 0 {
		t.Errorf("expected 0 users, got %d", n)
	}
}

func TestHydrate_AutoAsGeneric(t *testing.T) {
	input := makeMaxiInput([]string{"U:User(id:int|name)"}, []string{"U(1|Julie)"})
	res, err := api.ParseMaxiAutoAs[hydrateUser](input)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(res.Objects["U"]) != 1 {
		t.Fatalf("expected 1 user, got %d", len(res.Objects["U"]))
	}
	u := res.Objects["U"][0].(*hydrateUser)
	if u.Name != "Julie" {
		t.Errorf("name: want Julie, got %s", u.Name)
	}
}

func TestHydrate_AutoAsNoSchema(t *testing.T) {
	type NoSchema struct{ X int }
	_, err := api.ParseMaxiAutoAs[NoSchema]("X(1)")
	if err == nil {
		t.Fatal("expected error for type with no schema")
	}
	if !strings.Contains(err.Error(), "no schema") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHydrate_AutoAsMulti(t *testing.T) {
	input := makeMaxiInput(
		[]string{"U:User(id:int|name)", "O:Order(id:int|userId:U|total:decimal)"},
		[]string{"U(1|Julie)", "O(100|1|49.99)"},
	)
	res, err := api.ParseMaxiAutoAsMulti(input, []reflect.Type{
		reflect.TypeOf(hydrateUser{}),
		reflect.TypeOf(hydrateOrder{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Objects["U"][0].(*hydrateUser).Name != "Julie" {
		t.Error("expected Julie")
	}
	order := res.Objects["O"][0].(*hydrateOrder)
	if _, ok := order.UserID.(*hydrateUser); !ok {
		t.Errorf("expected userID resolved to *hydrateUser, got %T", order.UserID)
	}
}

func TestHydrate_AutoAsMultiNoSchema(t *testing.T) {
	type NoSchema struct{ X int }
	_, err := api.ParseMaxiAutoAsMulti("X(1)", []reflect.Type{reflect.TypeOf(NoSchema{})})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no schema") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHydrate_ExplicitRegistration(t *testing.T) {
	type Product struct {
		ID    int
		Title string
	}
	td := &core.MaxiTypeDef{
		Alias: "P",
		Name:  "Product",
		Fields: []*core.MaxiFieldDef{
			{Name: "id", TypeExpr: "int", Constraints: []core.ParsedConstraint{{Type: core.ConstraintID}}},
			{Name: "title"},
		},
	}
	_ = core.RegisterMaxiSchema((*Product)(nil), td)
	defer core.UnregisterMaxiSchema((*Product)(nil))

	input := makeMaxiInput([]string{"P:Product(id:int|title)"}, []string{"P(1|Widget)"})
	res, err := api.ParseMaxiAutoAs[Product](input)
	if err != nil {
		t.Fatal(err)
	}
	p := res.Objects["P"][0].(*Product)
	if p.ID != 1 {
		t.Errorf("id: want 1, got %d", p.ID)
	}
	if p.Title != "Widget" {
		t.Errorf("title: want Widget, got %s", p.Title)
	}
}

func TestHydrate_PointerFieldReference(t *testing.T) {
	type Addr struct {
		_      struct{} `maxi:"alias:A,name:Address"`
		ID     string   `maxi:"id"`
		Street string   `maxi:"street"`
	}
	type Cust struct {
		_    struct{}  `maxi:"alias:C,name:Customer"`
		ID   string    `maxi:"id"`
		Name string    `maxi:"name"`
		Addr *Addr     `maxi:"addr,type:A"`
	}

	input := makeMaxiInput(
		[]string{"A:Address(id|street)", "C:Customer(id|name|addr:A)"},
		[]string{"A(A1|Main St)", "C(C1|John|A1)"},
	)
	res, err := api.ParseMaxiAs(input, map[string]reflect.Type{
		"A": reflect.TypeOf(Addr{}),
		"C": reflect.TypeOf(Cust{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Objects["C"][0].(*Cust)
	if c.Addr == nil {
		t.Fatal("expected Addr to be resolved, got nil")
	}
	if c.Addr.Street != "Main St" {
		t.Errorf("street: want Main St, got %s", c.Addr.Street)
	}
}
