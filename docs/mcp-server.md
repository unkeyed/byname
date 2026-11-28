# MCP server grade

If your product has an MCP server, AI apps and agents use it to act for your users. Byname grades it against the MCP spec essentials and tells you what to fix. Add your server's URL under **Settings > MCP server** and Byname scores it from 0 to 100, gives it a letter, scores each of six categories, and lists every check with what it saw and, when it falls short, how to fix it.

The grade reads the server's metadata and its tool list. It never calls a tool. It is free: no model reads anything, and nothing goes through Treg.

## Grade your server

1. Open **Settings > MCP server**.
2. Paste the server's Streamable HTTP endpoint, such as `https://mcp.example.com/mcp`, and press **Save and grade**.
3. Wait up to 30 seconds. Most servers take a few.

Press **Grade again** after a deploy to see the new score. Byname grades only when someone asks. It does not grade on a schedule, so the page says when the grade it shows was made.

The URL must use `https`. Plain `http` works only for `localhost` and other loopback addresses, which Byname reaches only when `SOURCES_ALLOW_PRIVATE_IPS` is set (see [Configuration](configuration.md)). Do not put a key in the URL, as a query parameter or a user name: anyone who can read results sees the URL. Use the token instead.

Who can do what:

| | Viewer | Editor | Admin |
|---|---|---|---|
| See the grade | Yes | Yes | Yes |
| Set or remove the URL | | Yes | Yes |
| Grade again | | Yes | Yes |
| Set or delete the token | | | Yes |

An agent does the same with an API key or over MCP. See [Agents](agents.md).

## Servers behind sign-in

A server that answers `401` to a client without a token is graded in two parts.

- Without a token, Byname grades the connection, the sign-in itself (the Authorization category) and discoverability. It cannot open a session, so it cannot read the server metadata or the tools. Those three categories count as 0 with the note "Needs a token to grade", and the page marks the grade as partial.
- With a token, Byname opens a session as that token's user and grades the rest.

Only an admin can store a token, and only after confirming their sign-in within the last 15 minutes. Byname stores it encrypted with `ENCRYPTION_KEY`, sends it as `Authorization: Bearer <token>` to the server URL and to no other address, and never shows it again or returns it from any API. Use a token for an account that can only read. The grade lists tools and never calls one, but the token can do whatever the server lets it.

The sign-in checks always run without the token, so storing one never hides a broken sign-in. Changing the URL to another host, port or scheme deletes the token, so it never reaches a server it was not made for. Removing the server deletes the token too.

## How the score works

Each check earns all, part or none of its weight: **pass**, **partial** or **fail**. A check that does not apply is **skipped** and leaves the sums. A category's score is the weight its checks earned over the weight of the checks that applied, from 0 to 100.

The overall score is the mean of the categories that count, each weighing the same. Connection, server metadata, tool definitions and tool documentation always count. Authorization counts only when the server asks for sign-in. Discoverability would be left out if none of its checks could run, but the manifest and setup page checks run on every grade, so it counts too. When a category that always counts has no check that could run, such as when sign-in blocked the session, it counts as 0, and the grade says it is partial. Its `partial` field says why: `needs_token` when sign-in blocked the session and no token is stored, `no_session` when no session started for another reason, and `no_tools` when the server listed no tools. It is empty when the grade is complete.

| Letter | Score |
|---|---|
| A+ | 95 to 100 |
| A | 85 to 94 |
| B | 70 to 84 |
| C | 55 to 69 |
| D | 40 to 54 |
| F | Below 40 |

Checks that judge each tool give partial credit by share: when 3 of 4 tools have titles, the check earns three quarters of its weight, and its finding names up to five tools that fell short. Tool documentation scores each tool from 0 to 100 on its five checks and averages the tools, and the tools table shows each tool's score.

How long the session took to start is shown but not scored, so the same server gets the same grade from anywhere.

Byname keeps the newest 50 grades and draws their scores under **History**. Each grade records the version of the check list it used, such as `2026-11`, so a score that moved because the rules changed can be told apart from one that moved because the server did.

## Checks

Each check has an ID that never changes. The REST API and MCP tools return it with each result.

### Connection

How a client reaches the server and starts a session. A server that only speaks the older HTTP with SSE transport gets half the transport check. The protocol version check gives full credit for `2026-07-28`, three quarters for `2025-11-25`, half for `2025-06-18` and none for older versions. The last three checks need a session and are skipped without one.

| Check | Weight | What it looks for | Fix |
|---|---|---|---|
| `connection.https` | 10 | Served over HTTPS | Serve the endpoint over https. |
| `connection.transport` | 10 | Speaks Streamable HTTP | Serve MCP over Streamable HTTP: answer a POST to this URL with JSON or an event stream. |
| `connection.negotiate` | 10 | Session starts | Answer `server/discover` or `initialize` with serverInfo and capabilities. |
| `connection.version` | 6 | Current protocol version | Support protocol version 2026-07-28. |
| `connection.bad_version` | 3 | Rejects an unknown protocol version | Answer HTTP 400 when `MCP-Protocol-Version` names a version you do not support. |
| `connection.unknown_method` | 3 | Rejects unknown methods | Answer an unknown method with JSON-RPC error `-32601` (method not found). |
| `connection.origin` | 2 | Refuses foreign origins | Answer HTTP 403 when the Origin header names a site you do not trust. |

### Authorization

Whether a client can sign in on its own, following the MCP authorization spec. Skipped, and left out of the score, when the server does not ask for sign-in.

| Check | Weight | What it looks for | Fix |
|---|---|---|---|
| `authorization.www_authenticate` | 2 | Sign-in challenge names its metadata | Send `WWW-Authenticate: Bearer resource_metadata="<url>"` with every 401. |
| `authorization.resource_metadata` | 3 | Protected resource metadata | Serve RFC 9728 metadata at `/.well-known/oauth-protected-resource` with resource set to this URL and your `authorization_servers`. |
| `authorization.as_metadata` | 2 | Authorization server metadata | Serve RFC 8414 metadata at the authorization server's `/.well-known/oauth-authorization-server`. |
| `authorization.pkce` | 3 | PKCE with S256 | List S256 in `code_challenge_methods_supported`. |
| `authorization.registration` | 2 | Clients can register themselves | Support Client ID Metadata Documents or dynamic client registration (`registration_endpoint`). |

### Server metadata

What the server says about itself when a session starts. Clients show it when people add the server, and agents read the instructions to decide when to use it.

| Check | Weight | What it looks for | Fix |
|---|---|---|---|
| `metadata.name` | 10 | Server name | Set `serverInfo.name`. |
| `metadata.title` | 10 | Server title | Set `serverInfo.title` to the name people should see. |
| `metadata.description` | 20 | Server description | Set `serverInfo.description` to one sentence on what the server does. |
| `metadata.version` | 10 | Server version | Set `serverInfo.version`. |
| `metadata.website` | 10 | Website | Set `serverInfo.websiteUrl` to the server's home page. |
| `metadata.icon` | 10 | Icon | Add `serverInfo.icons` with an SVG, or an image with sizes of at least 64x64. |
| `metadata.instructions` | 20 | Instructions | Return instructions that tell an agent when and how to use the server. |
| `metadata.capabilities` | 10 | Capabilities declared | Declare the capabilities you serve: tools, resources or prompts. |

### Tool definitions

Whether each tool is defined so every client accepts it. Byname reads the tool list as the server sent it, so a tool a client library would drop silently is still graded.

| Check | Weight | What it looks for | Fix |
|---|---|---|---|
| `definitions.listed` | 3 | Lists tools | Declare the tools capability and list at least one tool. |
| `definitions.name_chars` | 3 | Tool names use safe characters | Use only `A-Z a-z 0-9 _ . -` in tool names, 1 to 128 long. |
| `definitions.name_unique` | 3 | Tool names are unique | Give every tool a distinct name. |
| `definitions.name_64` | 1 | Tool names fit 64 characters | Keep tool names to 64 characters; many clients cut or refuse longer ones. |
| `definitions.input_schema` | 3 | Input schemas are valid | Give every tool an `inputSchema` of type object, with `x-mcp-header` only on string, integer or boolean properties and each header name unique. |
| `definitions.output_schema` | 2 | Output schemas are objects | Make every `outputSchema` a JSON Schema of type object. |
| `definitions.descriptions` | 2 | Tools have descriptions | Give every tool a description. |
| `definitions.titles` | 1 | Tools have titles | Give every tool a title. |
| `definitions.annotations` | 1 | Tools declare every hint | Declare `readOnlyHint`, `destructiveHint`, `idempotentHint` and `openWorldHint` on every tool. |
| `definitions.desc_2kb` | 1 | Descriptions fit 2 KB | Keep each tool description under 2 KB and move detail into the input descriptions. |
| `definitions.hidden_chars` | 3 | No hidden characters | Remove invisible, control and bidirectional characters from tool names, descriptions and schemas. |
| `definitions.tool_budget` | 1 | Tool count fits an agent | Serve 40 tools or fewer; merge or split the rest into other servers. |
| `definitions.declared_lists` | 2 | Declared lists answer | Answer `resources/list` and `prompts/list` when you declare those capabilities. |

### Tool documentation

Whether an agent can tell from the definition alone what each tool does and how to call it. A tool with no inputs passes the inputs check.

| Check | Weight | What it looks for | Fix |
|---|---|---|---|
| `documentation.title` | 15 | Titled for people | Give each tool a title that says more than its name. |
| `documentation.description` | 35 | Described in full | Describe each tool in at least 40 characters: what it does, when to use it and what it returns. |
| `documentation.inputs` | 30 | Inputs described | Give every input property a type and a description. |
| `documentation.annotations` | 10 | Behavior hints | Declare the four behavior hints on each tool. |
| `documentation.output` | 10 | Output described | Add an `outputSchema` describing what each tool returns. |

### Discoverability

Whether people and agents can find the server. The manifest check also looks at `/mcp.json` on the endpoint's origin, and passes in part when the manifest it finds does not name this URL. The registry check is skipped, not failed, when the registry does not answer.

| Check | Weight | What it looks for | Fix |
|---|---|---|---|
| `discoverability.manifest` | 1 | Manifest names the endpoint | Serve `/.well-known/mcp.json` on the endpoint's origin naming this URL. |
| `discoverability.setup_page` | 1 | Setup page | Answer a browser (`Accept: text/html`) at this URL with a page on how to connect. |
| `discoverability.registry` | 1 | Listed in the MCP Registry | Publish the server to `registry.modelcontextprotocol.io` with this URL as a remote; a name under `io.github.*` may not be found by domain. |

## What the grade sends

The grade makes at most 40 requests, reads at most 8 MiB in all and 2 MiB from any one response, and stops after 30 seconds. Each request carries `User-Agent: Byname/<version>`. The grade sends:

1. One `initialize` request without the token, to find the transport and whether the server asks for sign-in.
2. When it does, requests for the protected resource metadata and the authorization server metadata it names.
3. Unless the server asks for sign-in and no token is stored, a session, with the token when one is stored: `initialize`, then `tools/list` page by page, then `resources/list` and `prompts/list` when the server declares them. Then four requests on that session: one with an unknown protocol version, one with an unknown method, and the same harmless request with and without a foreign `Origin`. When no session starts, one request for the older HTTP with SSE transport.
4. At the same time, without the token: a browser-style request to the URL, `/.well-known/mcp.json` and then `/mcp.json` on its origin, and a search of the official MCP Registry for the server's domain name.

It never sends `tools/call`, so no tool runs. Byname refuses private and loopback addresses after DNS, as it does for every address someone else chose, unless `SOURCES_ALLOW_PRIVATE_IPS` is set.
