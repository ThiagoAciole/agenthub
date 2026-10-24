# Arquitetura F0

## Fluxo

`cmd` valida flags e apresenta resultados. `platform` resolve diretórios base do usuário e do projeto. Adaptadores isolados em `providers` listam raízes por ferramenta; `discovery` lê apenas arquivos nessas raízes e normaliza os achados em `models`. `registry` ordena os itens e agrupa duplicações por hash de conteúdo.

```text
cmd -> platform + providers -> discovery -> models -> registry -> JSON/texto
```

## Escopo descoberto

| Provider | Global | Projeto |
| --- | --- | --- |
| Codex | `~/.codex` (ou `CODEX_HOME`) | `.codex`; `.codex/config.toml`; `AGENTS.md` |
| Claude Code | `~/.claude` | `.claude`; `.mcp.json` |
| OpenCode | diretório de configuração `opencode` | `.opencode`; `opencode.json` |

Os diretórios conhecidos são `agents`, `skills`, `commands`, `rules` e `prompts`, conforme cada provider. O AgentHub não percorre diretórios arbitrários do projeto.

## Segurança e limites

- `scan`, `list`, `status` e `doctor` não escrevem dados.
- `init` cria somente `agenthub/config.json`, sem sobrescrever arquivo existente.
- Para MCP, o scanner conserva apenas nomes de chaves em `mcpServers`/`servers` no JSON e cabeçalhos de servidor no TOML; valores são descartados.
- Duplicação compara bytes de arquivos Markdown. Nomes iguais, arquivos MCP e equivalência semântica não contam como duplicação.
- Na F0, não há sincronização, daemon, importação, edição de arquivos provider, integração remota ou banco de dados.
