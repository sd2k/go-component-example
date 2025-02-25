// Code generated by wit-bindgen-go. DO NOT EDIT.

package fetcher

import (
	"go.bytecodealliance.org/cm"
)

// This file contains wasmimport and wasmexport declarations for "sd2k:go-component-example@0.1.0".

//go:wasmexport fetch
//export fetch
func wasmexport_Fetch(url0 *uint8, url1 uint32) (result *cm.Result[string, string, string]) {
	url := cm.LiftString[string]((*uint8)(url0), (uint32)(url1))
	result_ := Exports.Fetch(url)
	result = &result_
	return
}
