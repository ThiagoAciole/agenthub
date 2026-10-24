# AgentHub

CLI Go para inventariar arquivos locais de agentes e configurações de ferramentas de IA. A F0 oferece descoberta somente leitura para Codex, Claude Code e OpenCode.

## Uso

```text
agenthub init
agenthub scan [--scope all|global|project] [--project DIR] [--json]
agenthub list [--scope all|global|project] [--project DIR] [--json]
agenthub providers [--json]
agenthub status [--scope all|global|project] [--project DIR] [--json]
agenthub doctor [--scope all|global|project] [--project DIR] [--json]
```

`init` cria `%APPDATA%/agenthub/config.json` no Windows ou `$XDG_CONFIG_HOME/agenthub/config.json` (padrão `~/.config`) em macOS/Linux. O arquivo é criado exclusivamente se ainda não existir. A configuração é própria do AgentHub.

Escopo global usa os diretórios padrão em HOME e o diretório de configuração do sistema. Escopo de projeto usa o diretório atual ou `--project`. A saída JSON usa os mesmos registros normalizados da listagem.

## Descoberta F0

Detecta arquivos Markdown de agents, skills, commands/prompts e rules, além de nomes de servidores MCP em configurações JSON ou no cabeçalho `[mcp_servers.<nome>]` do `config.toml` do Codex. Valores de configurações MCP não entram no inventário. Arquivos de mesmo conteúdo recebem referências de duplicação por SHA-256.

## Desenvolvimento

```text
gofmt -w .
go vet ./...
go test ./...
go build -o agenthub.exe .
```

Consulte [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) para limites e caminhos descobertos.
