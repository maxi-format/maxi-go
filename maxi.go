// Package maxi implements the MAXI format parser, dumper, and hydrator.
//
// Quick start:
//
//	result, err := maxi.ParseMaxi(input)
//	output, err  := maxi.DumpMaxi(data, maxi.DumpOptions{DefaultAlias: "U", Types: types})
//	schema       := maxi.GetMaxiSchema(myStruct)
//
// See the api/ and core/ sub-packages for the full public surface.
package maxi

import (
	"github.com/maxi-format/maxi-go/api"
	"github.com/maxi-format/maxi-go/core"
)

var ParseMaxi = api.ParseMaxi

var DumpMaxi = api.DumpMaxi

var DumpMaxiAuto = api.DumpMaxiAuto

var ParseMaxiAs = api.ParseMaxiAs

var ParseMaxiAutoAsMulti = api.ParseMaxiAutoAsMulti

var StreamMaxi = api.StreamMaxi

var GetMaxiSchema = core.GetMaxiSchema

var RegisterMaxiSchema = core.RegisterMaxiSchema

var UnregisterMaxiSchema = core.UnregisterMaxiSchema
