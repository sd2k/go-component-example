# Go WebAssembly Component Example

This example demonstrates how to use [wit-bindgen-go] to generate Go bindings for a WebAssembly component,
[tinygo] to build the component,
[wkg] to manage the component's dependencies,
and [wasmtime](https://github.com/bytecodealliance/wasmtime) to run the component.

## Building

### Dependencies

There are rather a lot of dependencies required for this:

- [Rust][rust]
- [wkg]
- [tinygo]
- [wit-bindgen-go]

There are two stages: building the component, then building (and running) the runtime.

### Building the component

```sh
just build-component
```

### Running inside Wasmtime

```sh
just run-host
```

[rust]: https://rustup.rs
[wkg]: https://github.com/bytecodealliance/wasm-pkg-tools
[tinygo]: https://github.com/tinygo-org/tinygo
[wit-bindgen-go]: https://github.com/bytecodealliance/go-modules
