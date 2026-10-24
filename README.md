# AgentHub

CLI Go para inventariar harnesses e manter uma biblioteca local de recursos de agentes. Descoberta é somente leitura; o gerenciamento local e a publicação inicial de skills seguem em F1 parcial.

## Uso

```text
agenthub init
agenthub scan [--scope all|global|project] [--project DIR] [--json]
agenthub list [--scope all|global|project] [--project DIR] [--json]
agenthub show <resource-id> [--scope all|global|project] [--project DIR] [--json]
agenthub find <query> [--scope all|global|project] [--project DIR] [--json]
agenthub add <local-path> --type agents|skills|prompts|commands|rules|memory|mcps|plugins|knowledge|profiles --dry-run [--scope project --project DIR]
agenthub add github:owner/repo --dry-run [--ref main] [--subpath skills/helper] [--scope project --project DIR]
agenthub add --plan add.json --yes [--scope project --project DIR]
agenthub import --from codex|claude|opencode [--scope project --project DIR] [--json]
agenthub import --from claude --plan import.json --select 1,3 --yes [--scope project --project DIR]
agenthub migrate --dry-run [--source DIR --destination DIR --legacy-lock FILE]
agenthub plan <skill-slug> --provider codex|claude-code|opencode --scope global|project
agenthub apply --plan plan.json --yes
agenthub rollback <deployment-id> --provider codex|claude-code|opencode --scope global|project --yes
agenthub update <resource-name> <local-resource> --type skills --scope global|project
agenthub update --plan update.json --scope global|project --yes
agenthub rename <current-name> --type skills --to <new-name> --scope global|project
agenthub rename <current-name> --plan rename.json --scope global|project --yes
agenthub remove <resource-name> --type skills --scope global|project
agenthub remove --plan remove.json --scope global|project --yes
agenthub restore <resource-name> --type skills --scope global|project
agenthub restore --plan restore.json --scope global|project --yes
agenthub history <resource-name> --type skills --scope global|project [--json]
agenthub deployments [--scope all|global|project] [--json]
agenthub deployments remove <deployment-id> --provider codex|claude-code|opencode --scope global|project
agenthub deployments remove --provider codex|claude-code|opencode --scope global|project --plan deployment-remove.json --yes
agenthub operations [--json]
agenthub operations recover <operation-id>
agenthub operations recover <operation-id> --plan recovery.json --yes
agenthub providers [--json]
agenthub status [--scope all|global|project] [--project DIR] [--json]
agenthub doctor [--scope all|global|project] [--project DIR] [--json]
```

`init` cria `~/.agenthub/config.json` e pode ser repetido sem alterar o arquivo existente. A configuração é estado privado do AgentHub.

Escopo global usa diretórios padrão em HOME. Escopo de projeto usa o diretório atual ou `--project`. `scan` lista recursos descobertos nos providers; `list` lista apenas recursos gerenciados no catálogo canônico, com escopo e proveniência. `find` pesquisa metadata e o texto de arquivos `.md`, `.markdown`, `.txt`, `.text`, `.rst`, `.adoc`, `.csv`, `.json`, `.yaml`, `.yml` e `.toml` em recursos `knowledge`; a saída contém somente os registros correspondentes, nunca trechos do documento.

## Descoberta F0

Detecta arquivos Markdown de agents, skills, commands/prompts e rules, além de nomes de servidores MCP em configurações JSON ou no cabeçalho `[mcp_servers.<nome>]` do `config.toml` do Codex. Valores de configurações MCP não entram no inventário. Arquivos de mesmo conteúdo recebem referências de duplicação por SHA-256.

## Gerenciamento local inicial

`add` gera prévia para origem local ou Git; a aplicação exige plano salvo e `--yes`. Para Git, aceita HTTPS e repositório local, fixa ref/commit/hash, importa só arquivos regulares do recurso selecionado e não executa scripts, hooks ou submódulos. A proveniência não armazena caminho absoluto de repositório local. HTTPS com credenciais embutidas e outros protocolos são recusados. A cópia local preserva arquivos auxiliares, registra hash em `~/.agents/.agenthub/registry.json`, não sobrescreve recursos existentes e recusa symlinks. `import` gera plano JSON com hashes e IDs dos candidatos; a aplicação exige o mesmo plano, `--yes` e seleção explícita com `--select`. Fontes alteradas depois da prévia e colisões de destino são recusadas. Prompts Codex são catalogados/importados como `prompts`; um arquivo isolado vira bundle `<slug>/<arquivo>.md` com hash verificado. Publicação de prompts nos providers não está implementada nem validada. Nenhum dos comandos aplica recursos nas configurações nativas. Metadados de terceiros, como `agents.lock.json`, permanecem fora do registry AgentHub.

`migrate --dry-run` inventaria `~/.agenthub/library` para `~/.agents`, calcula hashes e destaca destinos já existentes, colisões case-insensitive e registros conflitantes. `~/.agents/agenthub.lock.json` é tratado apenas como um caminho candidato para revisão manual: o schema original não foi localizado, então o conteúdo não é interpretado ou convertido. Nenhum arquivo é copiado e não há modo de aplicação neste comando.

`rename` move somente o recurso canônico após prévia, plano íntegro, hash revalidado e `--yes`. O `resourceId` e a revisão permanecem iguais; recursos novos recebem UUIDs e IDs legados são preservados. Deployments em providers não são renomeados nem removidos; planeje essas operações separadamente.

Os tipos de catálogo incluem agents, skills, prompts, commands, rules, memory, MCP, plugins, knowledge e profiles. Isso não significa suporte de instalação nos providers. Codex, Claude Code e OpenCode têm raízes de descoberta declaradas; publicação de qualquer tipo continua `not-tested`.

O fluxo de skills suporta `plan` read-only, `apply` aprovado e `rollback` protegido por hash. `apply` cria em destino vazio ou atualiza deployment AgentHub com ownership comprovado, salvando snapshot privado da versão anterior e journal por plano em `~/.agenthub/operations/`. `operations recover` planeja recuperação após falha: finaliza apenas deployment com hash confirmado ou restaura snapshot/quarentena íntegros em destino ausente; a gravação exige `--plan` e `--yes`. Divergência externa permanece bloqueada para resolução manual. Falhas anteriores à escrita podem ser tentadas de novo após revalidar as precondições. Colisões com conteúdo não gerenciado são recusadas. `update`, `remove` e `restore` apresentam prévia read-only e só alteram a biblioteca com plano salvo e `--yes`. `remove` move o canônico para snapshot privado e cria tombstone; cópias em providers permanecem intactas. `restore` exige destino livre e cria uma nova revisão. `history` mostra hashes e snapshots por revisão. `deployments remove` planeja e remove somente um bundle gerenciado, preservando snapshot e registro histórico.

F1 ainda está incompleta: faltam importação autenticada/SSH, recuperação automatizada de drift não comprovado e smoke tests em providers reais. F2 (cloud sync) e F3 (perfis, drift e TUI) ainda não estão implementadas. Consulte a [auditoria de migração](docs/MIGRATION-AUDIT.md) e o [plano incremental](docs/MIGRATION-PLAN.md).

## Desenvolvimento

```text
gofmt -w .
go vet ./...
go test ./...
go build -o agenthub.exe .
```

Consulte [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) para limites e caminhos descobertos.
