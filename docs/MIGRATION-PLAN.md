# Plano incremental de migração

Este plano preserva a CLI e o código existentes. Implementação futura é condicionada aos gates indicados; este documento não autoriza escrita em homes, providers, SSH ou VPS.

## Mapa para a arquitetura-alvo

| Atual | Alvo | Ação |
|---|---|---|
| `cmd/root.go` | `cmd/` presentation/use cases | Manter Cobra; reduzir lógica de operações conforme serviços concretos surjam. |
| `internal/models` | `internal/domain` ou modelos de contrato | Manter tipos de inventário compatíveis; evoluir Resource/Revision/Candidate/Deployment/Profile/Conflict com validação e schema versionado. |
| `internal/platform` | `internal/platform` | Centralizar home, `.agents`, `.agenthub`, projeto e escopos; manter `Roots` como compatibilidade temporária. |
| `internal/discovery` | `internal/discovery` | Manter scanner read-only; ampliar tipos e fixtures sem execução de conteúdo. |
| `internal/providers` | `internal/providers` adapters | Manter roots de descoberta; capabilities granulares começam `not-tested` até smoke test por versão. |
| `internal/registry` | catálogo/índice | Manter ordenação e hashes; evoluir sidecar versionado sem interpretar manifestos de terceiros. |
| `internal/library` | `internal/library` | Mantém cópia local/Git via preview/apply, revisão, update, tombstone, restore e `history`; seguem pendentes SSH/auth, importação de providers mais ampla e histórico cloud. |
| `internal/deployment` | `internal/deployment` | Contém plan/apply/rollback de skills com ownership; update salva snapshot privado anterior. `apply` registra operação por plano em `~/.agenthub/operations`; `operations recover` lista, planeja e aplica recuperação aprovada por hashes, incluindo finalize de commit íntegro ou restauração de snapshot para destino ausente. Drift sem hash comprovável segue manual. `deployments remove` preserva snapshot e registro. |
| `internal/cloud` | `internal/cloud` | Núcleo 3-way, plano integrado por resourceId (inclui rename), builder de snapshot isolado e transporte inicial para bare Git local (commit/push fast-forward-only/clone/fetch como candidato validado); ainda sem SSH, persistência, aprovação CLI, fila offline ou aplicação de conteúdo remoto. Outros pacotes futuros (`planner`, `storage`, `profiles`, `security`, `tui`) só surgem com fluxo concreto e testes. |

Pacotes de scanner, registry, provider roots e CLI de inventário podem permanecer. Não existe adapter de escrita que possa ser marcado como suportado.

## Domínio e persistência

O estado atual `Entry` preserva IDs legados derivados de tipo/slug e usa UUIDs em novos recursos; mantém origem, hash e revisões no registry. Os tipos de entidade foram introduzidos no pacote `models`, mas ainda são contratos sem persistência/invariantes completos.

Ordem sugerida: definir schemaVersion e Resource/Revision; gerar IDs estáveis; registrar bundles e hashes reproduzíveis; representar ImportCandidate sem adoção; adicionar Deployment/Operation/Conflict/Profile e migrations aditivas; só então conectar comandos. Revisões confirmadas nunca são editadas in-place.

## Paths, compatibilidade e metadados

- Canônico global: `~/.agents`; projeto: `<repo>/.agents` somente em escopo opt-in.
- Operação privada: `~/.agenthub` (config, state, backups, snapshots, staging, cache e logs).
- Metadata portável do AgentHub: `~/.agents/.agenthub/registry.json`; caminho agora usado pelo código de `library`. Validar leitura por ferramentas terceiras antes de depender desse namespace para conteúdo público.
- `agents.lock.json`, `settings.local.json`, `.sync-manifest.json` e nomes desconhecidos ficam byte a byte intocados.
- `~/.agents/agenthub.lock.json` é apenas um caminho legado candidato mencionado nesta migração; não foi localizada definição do schema nem fixture histórica. `migrate --dry-run` só informa hash e destino proposto para revisão manual, sem interpretar ou converter o conteúdo.
- `migrate --dry-run` enumera `~/.agenthub/library` por tipos e nomes reconhecidos, calcula hashes e aponta destinos presentes, colisões case-insensitive e entries já registradas; não copia. A aplicação e eventual tratamento de fontes desconhecidas seguem pendentes, após schema legado confirmado, backup e aprovação.
- `agenthub migrate --dry-run` foi executado contra os roots globais reais somente para leitura: `~/.agenthub/library` ausente, zero candidatos e `~/.agents/agenthub.lock.json` ausente. Nenhum dado foi copiado ou alterado. Não criar uma segunda biblioteca em `~/.agenthub`.
- O checkout arquivado do Harness Library declara que será greenfield e que não haverá recuperação de código legado; `~/.harness/library` e metadados PostgreSQL aparecem como arquitetura proposta, sem implementação/formato local comprovado. Isso não é fonte para inferir o formato AgentHub.
- O staging Git futuro fica em `~/.agenthub/git/`, com export allowlist derivado de `.agents`; nunca inicializar Git na árvore compartilhada.

## Semântica dos comandos

| Operação | Estado/gate pretendido |
|---|---|
| `scan` | Apenas leitura; sem mkdir, execução ou alteração de Git/configs. Teste de hash antes/depois. |
| `init` | Idempotente; criar apenas estado próprio. Não adotar nem modificar `.agents` que já exista. Ajustar layout XDG/APPDATA existente em migração compatível. |
| `import` | Descobrir candidatos e mostrar paths/hash/colisão; prévia JSON gera plano exato, seleção por ID e aprovação aplicam somente os itens escolhidos, após revalidar hashes. |
| `migrate --dry-run` | Enumerar raízes legadas com origem/destino/hash/colisão; somente prévia, sem escrita; locks legados desconhecidos exigem análise manual. |
| `add` | Incorporar local/Git com origem/revisão/hash, sem scripts; adicionar preview e limites antes de fontes remotas. |
| `plan` | Read-only: diffs, ownership, preconditions, riscos, hashes e validade. |
| `apply` | Verificar plano e hashes novamente, autorização/ownership, snapshot, escrita atômica, pós-hash e journal por destino. Falha parcial jamais sinaliza sucesso global. |
| `remove` / `rollback` | Tombstone e snapshot; preservar mudanças posteriores e arquivos desconhecidos. Nenhum remove recursivo sem posse comprovada. |
| `cloud sync` | Replica allowlist via staging isolado; offline/outbox e comparação base/local/remote. Não chama `apply`; sem force push/reset/clean. |
| `drift` / profiles | Comparação e seleção apenas; conflito exige resolução explícita. Alternar profile nunca remove conteúdo canônico. |

## Providers e critérios de suporte

O inventário local fornecido pelo usuário (`AUDITORIA_IA_HARNESS_SKILLS_MCP.md`, 24/10/2026) foi complementado nesta sessão por probes de versão de Claude Code e OpenCode. Presença e versão não provam publicação/carregamento de skills.

| Provider no código | Evidência local em 24/10/2026 | Skills | Outros tipos | Versão / smoke real |
|---|---|---|---|---|
| Codex | Instalado no snapshot da auditoria | `not-tested` em runtime | `not-tested` | 0.164.0 observado em 24/10/2026; não revalidado nesta sessão |
| Claude Code | Instalado; uma skill de usuário observada | `not-tested` em runtime | `not-tested` | 2.1.310 observado nesta sessão (24/10/2026); versão da auditoria: 2.1.296 |
| OpenCode | Instalado; skill compartilhada observada | `not-tested` em runtime | `not-tested` | 2.1.4 observado nesta sessão (24/10/2026); versão da auditoria: 2.1.4 |

Prioridade F1: usar Claude Code + OpenCode como primeiro par de fixtures, com `reverse-engineer-anything` como candidata observada em Claude e na árvore compartilhada. A documentação oficial atual do OpenCode descreve descoberta em `.agents/skills`, mas a auditoria e a versão instalada não provam carregamento real; `opencode debug skill` não existe no CLI 2.1.4 observado. Fazer smoke real apenas em ambiente descartável autorizado; Codex permanece terceiro adapter. Outros tipos continuam catalogáveis; não herdam capability de skills.

## Marcos e gates

1. **F0 — concluído parcialmente:** inventário/docs e regressões read-only; manter gaps explícitos. Gate: `go test`, `go vet`, `go build`, fixture de bytes inalterados.
2. **F1a — biblioteca/schema (parcial):** IDs UUID para recursos novos (IDs legados preservados), revisões, `list`/`show` do catálogo canônico, `find` por metadata e texto em formatos plain-text comuns de recursos `knowledge`, import preview/approval com plano hash-bound e seleção individual, discovery/import de bundles de skills por `SKILL.md` e prompts Codex, update com snapshot, rename canônico aprovado preservando resourceId/revisão, remove por tombstone e restore aprovado; `migrate --dry-run` inventaria árvore antiga e lock legado sem conversão. Rename não altera deployments em providers. Importação remota e aplicação de migração ainda pendentes. Publicação de prompts permanece não implementada/not-tested. Gate: Unicode/paths, colisões, symlink/junction, arquivos externos idênticos.
3. **F1b — deployment (parcial):** `plan/apply/rollback` para skills com ownership local, hash revalidado, snapshot privado em update, journal por plano e `operations recover` com prévia/aprovação. Falha pré-write pode ser retentada; recovery finaliza apenas commit íntegro, restaura snapshot verificado para destino ausente ou confirma que o estado-base permaneceu. Teste de falha injetada na troca e na restauração confirma journal `partial` e recuperação da revisão anterior. Drift não comprovado exige inspeção manual. Faltam smoke tests de dois providers. Gate continua: dois providers, drift, hash alterado entre plan/apply, falha parcial/rollback.
4. **F2 — sync:** núcleo 3-way, plano integrado, snapshot isolado e transporte local para bare Git implementados parcialmente em `internal/cloud`; fetch materializa somente candidato isolado validado, não aplica na canonical. Faltam SSH, CLI/aprovação persistida, aplicação de decisões, atualização end-to-end, dois roots isolados, fila offline/restart e push race. O snapshot compara hashes e bloqueia nomes/extensões e padrões comuns de credenciais, mas heurística não garante ausência de segredos. Gate: sem `--force`, sem Git na canonical.
5. **F3 — profiles/drift/TUI:** chamar serviços core já existentes; testes de conflito e preservação de conteúdo. TUI somente após CLI estável.
6. **F4 — hardening/release:** race quando disponível, cross-builds, restore ensaiado e documentação de limitações.

Rollback de código: cada etapa mantém formatos legados legíveis e migration aditiva com backup; nenhuma mudança de formato público sem versão. Rollback de filesystem futuro só restaura snapshot se hash atual ainda for o hash produzido pela operação, preservando edições concorrentes.

## Continuação F1a — identidade estável e rename

Novos recursos recebem UUID; registros antigos mantêm o ID gravado. `rename` exige preview, plano hash-bound e `--yes`, revalida o hash e move apenas o caminho canônico, preservando `resourceId` e revisão atual. Deployments não são movidos nem removidos; permanecem rastreáveis pelo mesmo ID e requerem ação explícita em separado. Fixtures cobrem rename aprovado, edição após preview, identidade/revisão preservadas e leitura de ID legado. O journal de deployment aceita UUID e o formato legado `skills:<slug>`.

## Aprovações manuais futuras

- Antes de qualquer aplicação futura, revisar um novo `agenthub migrate --dry-run`, confirmar schema/destino e autorizar explicitamente após backup/hash.
- Criar/selecionar repositório bare privado na VPS e autorizar configuração SSH antes de `cloud connect`; Git remoto lê conteúdo em claro.
- Em segundo dispositivo, fazer `cloud status`/preview, revisar colisões e conteúdo recebido, e só então planejar/aplicar providers localmente.

### F2 — aplicação remota mínima

Implementado executor de uma decisão remota por vez, condicionado a plano íntegro e candidato isolado validado. Materializa recurso novo/atualizado no mesmo tipo e nome, mantém IDs remotos, guarda backup em update e permite replay idempotente. Tombstone remoto exige correspondência exata de identidade/path/hash; rename, restauração e tombstone divergente continuam bloqueados. Testes focados passam. Próximos itens F2: aprovação/CLI persistida, integração do ciclo fetch-plan-apply, SSH, fila offline/restart e validação com dois roots/dispositivos.
