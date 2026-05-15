# MAXI Dumper (Go)

The `DumpMaxi` and `DumpMaxiAuto` functions serialize Go structs, maps, or parse
results back into MAXI text format.

---

## Table of Contents

1. [Overview](#overview)
2. [Input Modes](#input-modes)
3. [Schema Input](#schema-input)
4. [Struct-Tag Schema (Auto Dump)](#struct-tag-schema-auto-dump)
5. [`DumpMaxiAuto` — Schema from Tags](#dumpmaxiauto--schema-from-tags)
6. [Reference Collection](#reference-collection)
7. [Inline Objects vs. References](#inline-objects-vs-references)
8. [Inheritance](#inheritance)
9. [`DumpOptions` Reference](#dumpoptions-reference)
10. [Examples](#examples)

---

## Overview

```go
import "github.com/maxi-format/maxi-go/api"

out, err := api.DumpMaxi(data, api.DumpOptions{...})
```

`DumpMaxi` accepts data in several shapes and optional configuration through `DumpOptions`.
It emits a MAXI string that may contain:

- Directives (`@version`, `@schema`)
- Type definitions (schema section)
- A `###` separator
- Records (data section)

---

## Input Modes

`DumpMaxi` detects the input shape and routes to the appropriate internal path:

| Input type | Behavior |
|---|---|
| `*core.MaxiParseResult` | Round-trip — re-emits schema and records exactly as parsed |
| `[]map[string]any` | Requires `opts.DefaultAlias`; type info from `opts.Types` |
| `map[string][]map[string]any` | Each key is a record alias; type info from `opts.Types` |
| Any struct slice (via reflect) | Schema from `maxi:` struct tags or `opts.Types` |

### Round-trip (parse result)

```go
result, _ := api.ParseMaxi(input)
roundTripped, _ := api.DumpMaxi(result, api.DumpOptions{})
```

---

## Schema Input

The dumper needs to know field order and type information. Supply it through `opts.Types`:

```go
api.DumpOptions{
    DefaultAlias: "U",
    Types: []*core.MaxiTypeDef{{
        Alias: "U", Name: "User",
        Fields: []*core.MaxiFieldDef{
            {Name: "id",    TypeExpr: "int"},
            {Name: "name"},
            {Name: "email", DefaultValue: "unknown"},
        },
    }},
    IncludeTypes:      true,
    CollectReferences: true,
}
```

### External schema file

Reference an external `.mxs` schema file instead of embedding type definitions:

```go
out, _ := api.DumpMaxi(data, api.DumpOptions{
    DefaultAlias: "U",
    SchemaFile:   "schemas/users.mxs",
    IncludeTypes: false, // type defs come from the file
})
// Output:
// @schema:schemas/users.mxs
// ###
// U(1|Julie)
```

---

## Struct-Tag Schema (Auto Dump)

Attach schema metadata to your Go structs using `maxi:` struct tags.
The dumper discovers them automatically via reflection.

### Type-level tag

```go
type User struct {
    _        struct{} `maxi:"alias:U,name:User"`
    ID       int      `maxi:"id,type:int"`
    Name     string   `maxi:"name"`
    Email    string   `maxi:"email,default:unknown"`
}
```

The blank `_` field carries the `alias:` and `name:` for the whole type.
Every other exported field with a `maxi:` tag is included in declaration order.

### Field tag keys

| Key | Description |
|---|---|
| `type:<expr>` | TypeExpr: `int`, `decimal`, `float`, `str`, `bool`, `bytes`, `OtherAlias`, `OtherAlias[]` |
| `ann:<annotation>` | Annotation: `hex`, `base64`, `email`, `datetime`, `date`, `timestamp` |
| `default:<value>` | Default value for the field |
| `id` | Marks this field as the ID field |
| `required` | Marks this field as required |

### Explicit registration (for types you don't own)

```go
td := &core.MaxiTypeDef{
    Alias: "P", Name: "Product",
    Fields: []*core.MaxiFieldDef{
        {Name: "id",    TypeExpr: "int"},
        {Name: "title"},
    },
}
_ = core.RegisterMaxiSchema((*Product)(nil), td)
defer core.UnregisterMaxiSchema((*Product)(nil))
```

---

## `DumpMaxiAuto` — Schema from Tags

When your structs have `maxi:` tags (or are registered via `RegisterMaxiSchema`),
use `DumpMaxiAuto` — no `DefaultAlias` or `Types` needed:

```go
users := []User{
    {ID: 1, Name: "Julie"},
    {ID: 2, Name: "Matt"},
}

out, err := api.DumpMaxiAuto(users)
// Output:
// U:User(id:int|name|email=unknown)
// ###
// U(1|Julie)
// U(2|Matt)
```

For multiple types:

```go
data := map[string][]any{
    "U": {User{ID: 1, Name: "Julie"}},
    "O": {Order{OrderID: 100, Total: "49.99"}},
}
out, err := api.DumpMaxiAuto(data)
```

### How schema collection works

1. For each object `DumpMaxiAuto` calls `core.GetMaxiSchema(obj)` to retrieve the `*MaxiTypeDef`.
2. It recurses into all typed nested fields to collect schemas for referenced types
   (e.g. an `Address` nested inside a `Customer` is picked up automatically).
3. All collected schemas are merged with any `opts.Types` you supply — caller wins on conflict.
4. The merged types are forwarded to `DumpMaxi` — no logic duplication.

### Overriding auto-collected types

```go
override := &core.MaxiTypeDef{
    Alias:  "U",
    Name:   "CustomUser",
    Fields: []*core.MaxiFieldDef{
        {Name: "id", TypeExpr: "int"},
        {Name: "name"},
    },
}
out, _ := api.DumpMaxiAuto(users, api.DumpOptions{
    Types:        []*core.MaxiTypeDef{override},
    IncludeTypes: true,
})
```

---

## Reference Collection

When `opts.CollectReferences` is `true` (the default for `DumpMaxiAuto`), nested objects
with a schema type that has an ID field are **promoted to top-level records**:

1. The dumper walks all fields whose `typeExpr` points to another type in the schema.
2. If that type has an ID field and the nested object has a value for it, the object
   is promoted to its own `A(...)` record.
3. In the parent record the field value is replaced with just the ID.
4. This repeats iteratively — deeply nested objects are also promoted.

When `CollectReferences` is `false`, nested typed objects are always serialized inline
as `(val1|val2|...)` regardless of whether they have an ID.

---

## Inline Objects vs. References

Consider a `Customer` with an `Address` field:

| Condition | Output |
|---|---|
| `Address` has ID field, `CollectReferences: true` | Customer stores `A1`; separate `A(A1|...)` record emitted |
| `Address` has ID field, `CollectReferences: false` | Customer stores inline `(A1\|123 Main\|NYC)` |
| `Address` has **no** ID field | Always inlined: `(val1\|val2)` |

---

## Inheritance

If a `MaxiTypeDef` has `Parents`, the dumper resolves inherited fields before
serializing. Parent fields are prepended to the type's own fields, in declaration
order, with duplicate names skipped.

```go
parent := &core.MaxiTypeDef{
    Alias: "P", Name: "Person",
    Fields: []*core.MaxiFieldDef{
        {Name: "id", TypeExpr: "int"},
        {Name: "name"},
    },
}
employee := &core.MaxiTypeDef{
    Alias:   "E", Name: "Employee",
    Parents: []string{"P"},
    Fields:  []*core.MaxiFieldDef{{Name: "department"}},
}

// Serialized fields: id, name, department
out, _ := api.DumpMaxi(data, api.DumpOptions{
    DefaultAlias: "E",
    Types:        []*core.MaxiTypeDef{parent, employee},
})
```

---

## `DumpOptions` Reference

```go
type DumpOptions struct {
    DefaultAlias      string             // required when input is a slice or single object
    Types             []*core.MaxiTypeDef // type definitions
    IncludeTypes      bool               // emit type defs above ### (default true)
    SchemaFile        string             // emit @schema:<path> directive
    Version           string             // emit @version:<x> directive
    Multiline         bool               // pretty-print across multiple lines
    CollectReferences bool               // promote nested typed objects to top-level records
}

func DefaultDumpOptions() DumpOptions {
    return DumpOptions{
        IncludeTypes:      true,
        CollectReferences: true,
    }
}
```

---

## Examples

### 1. Array of maps with inline type definitions

```go
users := []map[string]any{
    {"id": 1, "name": "Julie"},
    {"id": 2, "name": "Matt", "email": nil},
}

out, _ := api.DumpMaxi(users, api.DumpOptions{
    DefaultAlias: "U",
    Types: []*core.MaxiTypeDef{{
        Alias: "U", Name: "User",
        Fields: []*core.MaxiFieldDef{
            {Name: "id", TypeExpr: "int"},
            {Name: "name"},
            {Name: "email", DefaultValue: "unknown"},
        },
    }},
    IncludeTypes: true,
})
// Output:
// U:User(id:int|name|email=unknown)
// ###
// U(1|Julie)
// U(2|Matt|~)
```

---

### 2. Multi-type map

```go
data := map[string][]map[string]any{
    "U": {{"id": 1, "name": "Julie"}},
    "O": {{"id": 100, "userId": 1, "total": "49.99"}},
}

out, _ := api.DumpMaxi(data, api.DumpOptions{
    Types: []*core.MaxiTypeDef{
        {Alias: "U", Name: "User",  Fields: []*core.MaxiFieldDef{{Name: "id", TypeExpr: "int"}, {Name: "name"}}},
        {Alias: "O", Name: "Order", Fields: []*core.MaxiFieldDef{
            {Name: "id", TypeExpr: "int"},
            {Name: "userId", TypeExpr: "int"},
            {Name: "total", TypeExpr: "decimal"},
        }},
    },
    IncludeTypes: true,
})
// Output:
// U:User(id:int|name)
// O:Order(id:int|userId:int|total:decimal)
// ###
// U(1|Julie)
// O(100|1|49.99)
```

---

### 3. Struct slice via `DumpMaxiAuto`

```go
type User struct {
    _     struct{} `maxi:"alias:U,name:User"`
    ID    int      `maxi:"id,type:int"`
    Name  string   `maxi:"name"`
    Email string   `maxi:"email,default:unknown"`
}

out, _ := api.DumpMaxiAuto([]User{
    {ID: 1, Name: "Julie"},
    {ID: 2, Name: "Matt", Email: "matt@maxi.org"},
})
// Output:
// U:User(id:int|name|email=unknown)
// ###
// U(1|Julie)
// U(2|Matt|matt@maxi.org)
```

---

### 4. Nested referenced objects

```go
type Address struct {
    _      struct{} `maxi:"alias:A,name:Address"`
    ID     string   `maxi:"id"`
    Street string   `maxi:"street"`
    City   string   `maxi:"city"`
}

type Customer struct {
    _       struct{} `maxi:"alias:C,name:Customer"`
    ID      string   `maxi:"id"`
    Name    string   `maxi:"name"`
    Address *Address `maxi:"address,type:A"`
}

addr := &Address{ID: "A1", Street: "123 Main St", City: "NYC"}
customers := []Customer{{ID: "C1", Name: "John", Address: addr}}

out, _ := api.DumpMaxiAuto(customers)
// Output:
// C:Customer(id|name|address:A)
// A:Address(id|street|city)
// ###
// C(C1|John|A1)
// A(A1|"123 Main St"|NYC)
```

---

### 5. Inline nested objects (`CollectReferences: false`)

```go
out, _ := api.DumpMaxiAuto(customers, api.DumpOptions{
    IncludeTypes:      true,
    CollectReferences: false,
})
// C(C1|John|(A1|"123 Main St"|NYC))
```

---

### 6. External schema file reference

```go
out, _ := api.DumpMaxi(data, api.DumpOptions{
    DefaultAlias:      "P",
    SchemaFile:        "sports.mxs",
    IncludeTypes:      false,
    CollectReferences: true,
})
// Output:
// @schema:sports.mxs
// ###
// P(1|Julie|forward|1998|1)
```

---

### 7. Multiline pretty-print

```go
out, _ := api.DumpMaxiAuto(users, api.DumpOptions{
    Multiline:    true,
    IncludeTypes: true,
})
// Output:
// U:User(
//   id:int|
//   name|
//   email=unknown
// )
// ###
// U(
//   1|
//   Julie
// )
```

---

### 8. Round-trip from a parse result

```go
result, _ := api.ParseMaxi(originalInput)

out, _ := api.DumpMaxi(result, api.DumpOptions{})
// out ≈ originalInput  (field order and values preserved)
```

---

### 9. Enum value aliases — compact wire tokens

When a field uses `enum[alias:value, ...]`, the dumper always emits the alias. You can pass either the alias or the full value as input.

```go
users := []map[string]any{
    {"id": 1, "name": "Alice", "role": "admin"}, // full value
    {"id": 2, "name": "Bob",   "role": "e"},     // alias also accepted
}

out, _ := api.DumpMaxi(users, api.DumpOptions{
    DefaultAlias: "U",
    Types: []*core.MaxiTypeDef{{
        Alias: "U", Name: "User",
        Fields: []*core.MaxiFieldDef{
            {Name: "id",   TypeExpr: "int"},
            {Name: "name"},
            {Name: "role", TypeExpr: "enum[a:admin,e:editor,v:viewer]"},
        },
    }},
})
// Output:
// U:User(id:int|name|role:enum[a:admin,e:editor,v:viewer])
// ###
// U(1|Alice|a)
// U(2|Bob|e)
```
