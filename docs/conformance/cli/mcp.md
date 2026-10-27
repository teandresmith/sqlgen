# CLI Requirements: MCP Server

<!-- seeding: in-progress -->
<!-- packages: cmd/sqlgen/mcp cmd/sqlgen/mcp/integration -->
<!-- matrix: mcp-surface rows=element cols=listed,output,deterministic,error -->

The MCP server (§31): configuration and `.mcp.json` emission, the tool, resource and prompt surface, the protocol and versioning. The `mcp-surface` matrix has one row per §31.3 tool, resource and prompt; `listed` is the element listed under its PRD name (`tools/list`, `resources/list`, `resources/templates/list` or `prompts/list`), `output` its §31.3 output or body for a valid input, `deterministic` a byte-equal response on repeat against an unchanged manifest, and `error` the coded failure §31.3 and §31.4 give it.
