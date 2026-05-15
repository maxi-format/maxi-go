# MAXI Parser (Go)

This document covers everything about parsing MAXI text into structured data —
from raw records, through streaming, all the way to typed Go struct instances
(object hydration).

---

## Table of Contents

1. [Overview](#overview)
2. [MAXI File Structure (Quick Recap)](#maxi-file-structure-quick-recap)
3. [`ParseMaxi` — Full In-Memory Parse](#parsemaxi--full-in-memory-parse)
4. [`StreamMaxi` — Streaming Parse](#streammaxi--streaming-parse)
5. [Parse Result Shape](#parse-result-shape)
6. [Struct-Tag Schema Annotation](#struct-tag-schema-annotation)
7. [`ParseMaxiAs` — Parse into Struct Instances](#parsemaxias--parse-into-struct-instances)
8. [`ParseMaxiAutoAs` — Auto-Resolve Structs](#parsemaxiautoas--auto-resolve-structs)
9. [Reference Resolution during Hydration](#reference-resolution-during-hydration)
10. [`ParseOptions` Reference](#parseoptions-reference)
11. [Examples](#examples)

---

## Overview

The parser converts MAXI text into one of two output shapes:

| Function | Output |
|---|---|
| `ParseMaxi` | `*MaxiParseResult` — schema + raw records (positional values) |
| `StreamMaxi` | `*MaxiStreamResult` — schema immediately, then a lazy `iter.Seq` of records |
| `ParseMaxiAs` | `*MaxiHydrateResult` — records hydrated into concrete Go struct instances |
| `ParseMaxiAutoAs[T]` | Same, with alias resolved from `maxi:` struct tags on `T` |

---

## MAXI File Structure (Quick Recap)

```
U:User(id:int|name|email=unknown)    ← type definitions (schema section)
O:Order(id:int|user:U|total:decimal)
###                                   ← separator
U(1|Julie|julie@example.com)          ← records (data section)
O(100|1|49.99)
```

- Everything **above** `###` is the schema section (type defs, directives like `@version`, `@schema`).
- Everything **below** `###` is the records section.
- If there is no `###`, the parser auto-detects whether the input is schema-only or records-only.

---

## `ParseMaxi` — Full In-Memory Parse

```go
import "github.com/maxi-format/maxi-go/api"

result, err := api.ParseMaxi(input)
// or with options:
opts := core.DefaultParseOptions()
opts.AllowUnknownTypes = core.UnknownTypesError
result, err = api.ParseMaxi(input, opts)
```

Parses the full input at once. Returns a `*core.MaxiParseResult` containing:
- `result.Schema` — types, directives, imports
- `result.Records` — slice of `*core.MaxiRecord` (positional values, schema-typed)
- `result.Warnings` — recoverable issues found during parsing
- `result.ObjectRegistry` — populated when any field references another type; cast to `api.ObjectRegistry` via `api.GetObjectRegistry(result)`

### What the parser does internally

1. **Split sections** at `###`
2. **Parse schema section** — type definitions, `@version`, `@schema` imports (loaded via `opts.LoadSchema`)
3. **Parse records section** — each record is matched to its type def; values are coerced (`int`, `bool`, `decimal`, etc.)
4. **Enum values pre-cached** on each `MaxiFieldDef.EnumValues` at schema-parse time (no regexp per record)
5. **Build object registry** — if any field references another type, an `ObjectRegistry` (alias → id-str → field map) is built
6. **Validate references** — unresolved references emit a warning (lax) or return an error (strict)

---

## `StreamMaxi` — Streaming Parse

For large files where you don't want all records in memory at once.

```go
import "github.com/maxi-format/maxi-go/api"

stream, err := api.StreamMaxi(input)
if err != nil {
    log.Fatal(err)
}

// Schema is fully available before iterating
fields := stream.Schema.GetType("U").Fields

// Iterate lazily (Go 1.23+ range-over-func)
for rec := range stream.Records() {
    fmt.Println(rec.Alias, rec.Values)
}

// Or iterate with explicit error handling
for rec, err := range stream.RecordsWithError() {
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(rec.Alias, rec.Values)
}

// Warnings accumulate as records are consumed
fmt.Println(stream.Warnings())
```

- The schema section is parsed **eagerly** and available immediately on the returned `*MaxiStreamResult`.
- Records are yielded **lazily** as you range over them.
- The `iter.Seq` pull model means you can `break` early with no goroutine leaks.

---

## Parse Result Shape

### `MaxiParseResult`

```go
type MaxiParseResult struct {
    Schema         *MaxiSchema
    Records        []*MaxiRecord
    Warnings       []*MaxiError
    ObjectRegistry any  // cast via api.GetObjectRegistry(result)
}
```

### `MaxiRecord`

```go
type MaxiRecord struct {
    Alias      string   // type alias, e.g. "U"
    Values     []any    // positional values, schema-coerced
    LineNumber  int
}
```

### `MaxiSchema`

```go
schema.GetType("U")          // → *MaxiTypeDef (nil if not found)
schema.HasType("U")          // → bool
schema.Types()               // → []*MaxiTypeDef (all registered types)
schema.ResolveTypeAlias("User") // → "U"  (name → alias lookup)
```

### `MaxiTypeDef`

```go
typeDef.Alias              // "U"
typeDef.Name               // "User"
typeDef.Parents            // []string{"P"}
typeDef.Fields             // []*MaxiFieldDef
typeDef.IDField()          // → *MaxiFieldDef (nil if none)
typeDef.IDFieldIndex()     // → int (-1 if none)
```

### `MaxiFieldDef`

```go
field.Name                 // "email"
field.TypeExpr             // "str", "int", "U", "O[]", etc.
field.Annotation           // "hex", "base64", "email", etc.
field.Constraints          // []ParsedConstraint
field.DefaultValue         // any
field.EnumValues           // []string  (pre-cached for enum fields)
field.IsRequired()         // bool
field.IsID()               // bool
```

### `ObjectRegistry`

```go
reg := api.GetObjectRegistry(result)  // api.ObjectRegistry = map[string]map[string]map[string]any

user := reg["U"]["1"]          // field-value map for User id=1
fmt.Println(user["name"])      // "Julie"
```

Fields with `@hex` or `@base64` annotation are keyed as `name_hex` / `name_base64`.

---

## Struct-Tag Schema Annotation

The Go registry uses struct field tags to describe a MAXI schema without any
code generation or external descriptors.

### Type-level tag

Place a blank field on your struct with a `maxi:` tag that contains `alias:` and `name:`:

```go
type User struct {
    _        struct{} `maxi:"alias:U,name:User"`
    ID       int      `maxi:"id,type:int"`
    Name     string   `maxi:"name"`
    Email    string   `maxi:"email,default:unknown"`
}
```

### Field tags

| Tag key | Description |
|---|---|
| `type:<expr>` | TypeExpr, e.g. `int`, `decimal`, `str`, `bool`, `bytes`, `OtherAlias`, `OtherAlias[]` |
| `ann:<annotation>` | Annotation, e.g. `hex`, `base64`, `email`, `datetime` |
| `default:<value>` | Default value string (parsed to the appropriate type) |
| `id` or `type:id` | Marks the field as the ID field (constraint `id`) |
| `required` | Marks the field as required (constraint `required`) |

The blank `_` field carries the type-level alias/name. All other exported fields whose tag
starts with a field name are picked up in declaration order.

### Explicit registration (for types you don't own)

```go
import "github.com/maxi-format/maxi-go/core"

td := &core.MaxiTypeDef{
    Alias: "P",
    Name:  "Product",
    Fields: []*core.MaxiFieldDef{
        {Name: "id",    TypeExpr: "int"},
        {Name: "title"},
        {Name: "price", TypeExpr: "decimal"},
    },
}
_ = core.RegisterMaxiSchema((*Product)(nil), td)

// Look up later:
td = core.GetMaxiSchema(&Product{})
```

---

## `ParseMaxiAs` — Parse into Struct Instances

```go
import (
    "reflect"
    "github.com/maxi-format/maxi-go/api"
)

classMap := map[string]reflect.Type{
    "U": reflect.TypeOf(User{}),
    "O": reflect.TypeOf(Order{}),
}

result, err := api.ParseMaxiAs(input, classMap)
```

Returns `*api.MaxiHydrateResult`:

```go
type MaxiHydrateResult struct {
    Objects  map[string][]any   // alias → slice of hydrated instances
    Schema   *core.MaxiSchema
    Warnings []*core.MaxiError
}
```

Fields are mapped by position (schema field order). Cross-reference fields are resolved
in a second pass: if field `typeExpr` points to another alias in the schema,
the scalar ID is replaced with the hydrated instance.

---

## `ParseMaxiAutoAs` — Auto-Resolve Structs

Convenience generic variant — the alias is inferred from the `maxi:` struct tag on `T`.

```go
result, err := api.ParseMaxiAutoAs[User](input)
users := result.Objects["U"]  // []any  (each element is a *User)
```

For multiple types, pass options with a pre-built class map or use `ParseMaxiAs` directly.

---

## Reference Resolution during Hydration

After all records are hydrated into instances the hydrator performs a **second pass**:

1. All `U` records are hydrated into `User` values and indexed by their id field.
2. All `O` records are hydrated into `Order` values.
3. Each `Order`'s `user` field has `typeExpr: "U"` — the hydrator looks up the scalar `1` in the `User` instance map and replaces the field value with the actual `User`.

Forward references are resolved automatically because the second pass runs after all records have been parsed.

---

## `ParseOptions` Reference

```go
opts := core.DefaultParseOptions()
```

| Field | Type | Default | Description |
|---|---|---|---|
| `AllowAdditionalFields` | `AdditionalFieldsMode` | `"ignore"` | Extra fields beyond the schema: `"ignore"`, `"warning"`, `"error"` |
| `AllowMissingFields` | `MissingFieldsMode` | `"null"` | Missing required fields: `"null"`, `"warning"`, `"error"` |
| `AllowTypeCoercion` | `TypeCoercionMode` | `"coerce"` | Type mismatches: `"coerce"`, `"warning"`, `"error"` |
| `AllowConstraintViolations` | `ConstraintViolationsMode` | `"warning"` | Constraint failures: `"warning"`, `"error"` |
| `AllowForwardReferences` | `bool` | `true` | Allow references to records not yet seen |
| `AllowUnknownTypes` | `UnknownTypesMode` | `"warning"` | Unknown type alias: `"ignore"`, `"warning"`, `"error"` |
| `LoadSchema` | `func(string) (string, error)` | `nil` | Resolver for `@schema:` import directives |

---

## Examples

### 1. Basic `ParseMaxi` — raw records

```go
input := `
U:User(id:int|name|email=unknown)
###
U(1|Julie|julie@example.com)
U(2|Matt)
`

result, err := api.ParseMaxi(input)
// result.Records[0].Values → [1, "Julie", "julie@example.com"]
// result.Records[1].Values → [2, "Matt", "unknown"]  ← default filled in
```

---

### 2. `StreamMaxi` — lazy iteration

```go
stream, err := api.StreamMaxi(input)
if err != nil { log.Fatal(err) }

// Schema ready before first record
fmt.Println(stream.Schema.GetType("U").Fields[0].Name) // "id"

for rec := range stream.Records() {
    fmt.Println(rec.Alias, rec.Values)
}
```

---

### 3. `@schema` import

```go
opts := core.DefaultParseOptions()
opts.LoadSchema = func(path string) (string, error) {
    return os.ReadFile(path)
}

result, err := api.ParseMaxi("@schema:schemas/users.mxs\n###\nU(1|Julie)", opts)
```

---

### 4. Object registry — resolve references manually

```go
input := `
U:User(id:int|name|email)
O:Order(id:int|user:U|total:decimal)
###
U(1|Julie|julie@maxi.org)
O(100|1|49.99)
`

result, _ := api.ParseMaxi(input)
reg := api.GetObjectRegistry(result)   // api.ObjectRegistry

// User id=1
user := reg["U"]["1"]
fmt.Println(user["name"])  // "Julie"

// Order user field stores the scalar reference id
order := result.Records[1]
fmt.Println(order.Values[1])  // int64(1)
```

---

### 5. `ParseMaxiAs` — hydrate into structs

```go
type User struct {
    _    struct{} `maxi:"alias:U,name:User"`
    ID   int      `maxi:"id,type:int"`
    Name string   `maxi:"name"`
}

type Order struct {
    _     struct{} `maxi:"alias:O,name:Order"`
    ID    int      `maxi:"id,type:int"`
    User  *User    `maxi:"user,type:U"`
    Total string   `maxi:"total,type:decimal"`
}

classMap := map[string]reflect.Type{
    "U": reflect.TypeOf(User{}),
    "O": reflect.TypeOf(Order{}),
}

hydrateResult, err := api.ParseMaxiAs(input, classMap)

users := hydrateResult.Objects["U"]   // []any  (each is *User)
orders := hydrateResult.Objects["O"]  // []any  (each is *Order)

u := users[0].(*User)
o := orders[0].(*Order)
fmt.Println(u.Name)          // "Julie"
fmt.Println(o.User.Name)     // "Julie"  ← reference resolved
```

---

### 6. `ParseMaxiAutoAs` — zero-config with struct tags

```go
hydrateResult, err := api.ParseMaxiAutoAs[User](input)
users := hydrateResult.Objects["U"]
fmt.Println(users[0].(*User).Name)  // "Julie"
```

---

### 7. Strict mode — error on any constraint violation

```go
opts := core.DefaultParseOptions()
opts.AllowConstraintViolations = core.ConstraintViolationsError
opts.AllowMissingFields = core.MissingFieldsError
opts.AllowAdditionalFields = core.AdditionalFieldsError

_, err := api.ParseMaxi(input, opts)
if err != nil {
    var me *core.MaxiError
    if errors.As(err, &me) {
        fmt.Println(me.Code, me.Message)
    }
}
```

---

### 8. Explicit schema registration for external types

```go
type Product struct { ID int; Title string }

core.RegisterMaxiSchema((*Product)(nil), &core.MaxiTypeDef{
    Alias: "P", Name: "Product",
    Fields: []*core.MaxiFieldDef{
        {Name: "id", TypeExpr: "int"},
        {Name: "title"},
    },
})

result, _ := api.ParseMaxiAutoAs[Product](input)
```

---

### 9. Enum value aliases

Enum fields may use short aliases as wire tokens. The parser always returns the full semantic value.

```go
input := `
U:User(id:int|name|role:enum[a:admin,e:editor,v:viewer])
###
U(1|Alice|a)
U(2|Bob|v)
`

result, _ := api.ParseMaxi(strings.TrimSpace(input))

fmt.Println(result.Records[0].Values[2]) // "admin" - alias 'a' expanded
fmt.Println(result.Records[1].Values[2]) // "viewer" - alias 'v' expanded
```

`enum<int>` aliases work the same way — the parsed value is always the integer:

```go
input := `
D:Device(id:int|name|state:enum<int>[O:900,I:910,R:1000,E:999])
###
D(1|sensor-A|R)
`

result, _ := api.ParseMaxi(strings.TrimSpace(input))

fmt.Println(result.Records[0].Values[2]) // 1000 - alias 'R' expanded to int
```

Wire tokens that are neither a declared alias nor the full value trigger a constraint violation (E303).
