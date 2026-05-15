# maxi-go

Go library for parsing and dumping **MAXI schema + records**.

Module: `github.com/maxi-format/maxi-go`  
Go version: **1.25+**  
Zero external dependencies.

## Install

```bash
go get github.com/maxi-format/maxi-go
```

## API overview

| Function | Description |
|---|---|
| `api.ParseMaxi(input, opts?)` | Parse MAXI text → `*MaxiParseResult` (schema + raw records) |
| `api.StreamMaxi(input, opts?)` | Parse schema eagerly, yield records lazily via `iter.Seq` |
| `api.ParseMaxiAs(input, classMap, opts?)` | Parse + hydrate records into Go struct instances |
| `api.ParseMaxiAutoAs[T](input, opts?)` | Same, with alias inferred from `maxi:` struct tags |
| `api.DumpMaxi(data, opts?)` | Serialize objects / parse results → MAXI text |
| `api.DumpMaxiAuto(data, opts?)` | Same, with schema inferred from `maxi:` struct tags |
| `core.RegisterMaxiSchema(ptr, def)` | Register a `*MaxiTypeDef` for a type you don't own |
| `core.GetMaxiSchema(v)` | Look up a registered or tag-derived schema |

## Quick start

### Parse

```go
import "github.com/maxi-format/maxi-go/api"

input := `
U:User(id:int|name|email)
###
U(1|Julie|julie@maxi.org)
`

res, err := api.ParseMaxi(input)
if err != nil {
    log.Fatal(err)
}
fmt.Println(res.Records[0].Values) // [1 Julie julie@maxi.org]
```

### Dump

```go
import (
    "github.com/maxi-format/maxi-go/api"
    "github.com/maxi-format/maxi-go/core"
)

users := []map[string]any{
    {"id": 1, "name": "Julie", "email": "julie@maxi.org"},
}

out, err := api.DumpMaxi(users, api.DumpOptions{
    DefaultAlias: "U",
    Types: []*core.MaxiTypeDef{{
        Alias: "U", Name: "User",
        Fields: []*core.MaxiFieldDef{
            {Name: "id", TypeExpr: "int"},
            {Name: "name"},
            {Name: "email"},
        },
    }},
    IncludeTypes: true,
})
```

### Struct-tag schema (auto dump/parse)

```go
type User struct {
    _     struct{} `maxi:"alias:U,name:User"`
    ID    int      `maxi:"id,type:int"`
    Name  string   `maxi:"name"`
    Email string   `maxi:"email,default:unknown"`
}

out, err := api.DumpMaxiAuto([]User{
    {ID: 1, Name: "Julie", Email: "julie@maxi.org"},
})
```

### Stream

```go
stream, err := api.StreamMaxi(input)
if err != nil { log.Fatal(err) }

// Schema fully available before iterating
fields := stream.Schema.GetType("U").Fields

// Iterate lazily
for rec := range stream.Records() {
    fmt.Println(rec.Alias, rec.Values)
}
```

## Documentation

- **[docs/parser.md](docs/parser.md)** — full parser guide: `ParseMaxi`, `StreamMaxi`, `ParseMaxiAs`, `ParseMaxiAutoAs`, options, examples
- **[docs/dumper.md](docs/dumper.md)** — full dumper guide: `DumpMaxi`, `DumpMaxiAuto`, struct tags, references, options, examples

## MAXI format (quick reference)

```
U:User(id:int|name|email=unknown)   ← type definition
###                                  ← section separator
U(1|Julie|~)                         ← record  (~ = explicit null)
```

- Omitted trailing fields use their declared default value.
- See the [MAXI spec](../maxi/SPEC.md) for the full format definition.

## Test

```bash
go test ./...
```

Conformance tests against the shared `maxi-testdata` suite run automatically.

## License

Released under the [MIT License](./LICENSE).
