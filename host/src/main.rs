use std::path::PathBuf;

use anyhow::Context;
use wasmtime::{component::*, Config, Engine, Store};
use wasmtime_wasi::{IoView, WasiCtx, WasiCtxBuilder, WasiView};
use wasmtime_wasi_http::{WasiHttpCtx, WasiHttpView};

wasmtime::component::bindgen!({
    path: "../wit",
    world: "fetcher",
    async: true
});

pub async fn fetch(path: PathBuf, url: String) -> wasmtime::Result<String> {
    let mut config = Config::default();
    config.wasm_component_model(true);
    config.async_support(true);
    let engine = Engine::new(&config)?;
    let mut linker = Linker::new(&engine);

    // Add the command world (aka WASI CLI) to the linker
    wasmtime_wasi::add_to_linker_async(&mut linker).context("Failed to link command world")?;
    wasmtime_wasi_http::add_only_http_to_linker_async(&mut linker)
        .context("Failed to link command world")?;
    let wasi_view = ServerWasiView::new();
    let mut store = Store::new(&engine, wasi_view);

    let component = Component::from_file(&engine, path).context("Component file not found")?;

    let instance = Fetcher::instantiate_async(&mut store, &component, &linker)
        .await
        .context("Failed to instantiate the example world")?;
    instance
        .call_fetch(&mut store, &url)
        .await
        .context("Failed to call add function")?
        .map_err(|e| anyhow::anyhow!("fetch failed: {}", e))
}

struct ServerWasiView {
    table: ResourceTable,
    http: WasiHttpCtx,
    ctx: WasiCtx,
}

impl ServerWasiView {
    fn new() -> Self {
        let table = ResourceTable::new();
        let ctx = WasiCtxBuilder::new()
            .inherit_stdio()
            .allow_ip_name_lookup(true)
            .allow_blocking_current_thread(true)
            .build();
        let http = WasiHttpCtx::new();

        Self { table, ctx, http }
    }
}

impl IoView for ServerWasiView {
    fn table(&mut self) -> &mut ResourceTable {
        &mut self.table
    }
}

impl WasiView for ServerWasiView {
    fn ctx(&mut self) -> &mut WasiCtx {
        &mut self.ctx
    }
}

impl WasiHttpView for ServerWasiView {
    fn ctx(&mut self) -> &mut WasiHttpCtx {
        &mut self.http
    }
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let path = std::env::args().nth(1).unwrap();
    let url = std::env::args().nth(2).unwrap();
    let result = fetch(PathBuf::from(path), url).await?;
    println!("{}", result);
    Ok(())
}
