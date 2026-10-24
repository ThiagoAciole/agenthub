# Auditoria da F0

## Estado verificado

- Repositório sem commits; arquivos existentes aparecem como não rastreados. O binário `agenthub.exe` já existia e foi preservado.
- CLI Cobra com `init`, `scan`, `list`, `providers`, `status` e `doctor`.
- Providers: Codex, Claude Code e OpenCode, com raízes globais e de projeto.
- Scanner identifica Markdown em diretórios conhecidos, regras `AGENTS.md` e nomes de servidores MCP sem guardar os valores de configuração.
- Registry ordena resultados e agrupa duplicações por SHA-256.
- `go test ./...`, `go vet ./...` e `go build ./...` passaram na auditoria.

## Limitações relevantes para F1-F3

- Modelo contém apenas provider, tipo, nome, origem e caminho; não há biblioteca canônica nem lockfile.
- Não há importação, recursos locais, planejamento, instalação, backups, manifestos, histórico ou rollback.
- Adaptadores expõem apenas raízes de descoberta; não há leitura/normalização/publicação por provider.
- `init` usa a pasta de configuração do sistema, distinta do layout canônico planejado.
- Não há Git/SSH, sync, validação de arquivos sensíveis, perfis, drift ou TUI.
- Os testes atuais cobrem scanner, registry e comandos existentes, mas não operações de gerenciamento.

## Escopo e segurança

F0 é somente leitura em scan/list/status/doctor. `init` cria exclusivamente sua configuração com criação exclusiva. Nenhuma alteração funcional foi necessária para a F0 passar as verificações solicitadas.

## Atualização pela auditoria de migração (2026-10-24)

O texto acima registra a auditoria histórica anterior à implementação local de `add`/`import`. O estado atual e os gates desta migração estão em [MIGRATION-AUDIT.md](MIGRATION-AUDIT.md). `init` cria estado privado em `~/.agenthub/config.json` e retorna sem alterar a configuração existente; `add` apresenta plano local/Git e registra metadados no namespace `~/.agents/.agenthub/registry.json` após aprovação; `import` apresenta prévia por padrão. A biblioteca local tem update com snapshot, remove por tombstone e restore aprovado. F1 segue incompleta: Git autenticado/SSH, journal de falha parcial, smoke tests de providers e sync continuam ausentes.
