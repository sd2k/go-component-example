
# Build the Wasm component.
build-component:
  just component/build

# Build the host application which runs the component.
build-host:
  just host/build

# Run the host application.
run-host:
  just host/run
