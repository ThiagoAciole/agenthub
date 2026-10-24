# Auditoria de migração — AgentHub

Data: 2026-10-24. Base de comparação: `AGENTS.md`, `PRD.md` e `ARCHITECTURE.md` atuais. Auditoria feita no checkout local, sem acessar ou alterar home, providers, SSH ou VPS.

## Estado inicial observado antes das alterações

- `git status --short` inicial: `README.md`, `cmd/root.go`, `cmd/root_test.go` modificados; `AGENTS.md`, `ARCHITECTURE.md`, `PRD.md`, `docs/F0-REVIEW.md` e `internal/library/` não rastreados. `agenthub.exe` já estava na raiz e foi preservado.
- CLI: `init`, `scan`, `list`, `providers`, `status`, `doctor`, `add` e `import` (`cmd/root.go`).
- Núcleo: descoberta de agents/skills/commands/rules e nomes MCP (`internal/discovery/scan.go`); agrupamento duplicado por hash (`internal/registry/registry.go`); providers Codex, Claude Code e OpenCode só declaram raízes (`internal/providers/*.go`).
- Biblioteca local adicionada no estado existente: cópia recursiva e hash, com rejeição de symlinks (`internal/library/library.go`). Antes deste ajuste, guardava `agenthub.lock.json` diretamente na raiz `.agents` e `import` copiava sem uma etapa de aprovação.
- Estado privado: `init` cria config no diretório de configuração do sistema, com `O_EXCL`; segunda execução falha em vez de ser idempotente (`cmd/root.go`).
- Não existem schemas versionados, migrações, deployment/ownership, plano/apply, snapshots, rollback, update/remove, cloud Git/SSH, profiles, drift ou TUI. Não foi localizado código que use `~/.agenthub/library` nem Git dentro de `.agents`.
- Não há prova de versão instalada/consumo real de recursos pelos providers. A lista atual é descoberta/configuração, não suporte de publicação.

## Matriz de implementação

| Área | Estado | Evidência / gap / risco |
|---|---|---|
| `scan` | Parcial | Scanner read-only para raízes declaradas; diferencia `prompts` Codex de `commands` e reconhece skills por `SKILL.md`, sem transformar `README.md`/referências em candidatos separados. OpenCode inclui `.agents/skills` global e de projeto conforme documentação do provider. Ignora providers desconhecidos e não cobre todos os tipos nem limites completos de filesystem. |
| `init` | Alinhado nesta etapa | Agora cria `~/.agenthub/config.json` idempotentemente em teste isolado. |
| `list` / `show` / `find` | Parcial | `list` exibe recursos vivos do registry canônico por escopo; `show <resource-id>` exibe proveniência/revisão e exige escopo se o mesmo ID estiver registrado em mais de um; `find` procura metadata e texto em formatos textuais de recursos `knowledge`, sem imprimir conteúdo. `scan` mantém inventário read-only dos providers. Busca semântica e formatos binários ainda não são suportados. |
| `providers` | Parcial | Lista adapters como `registered`, runtime `not-probed` e publicação `not-tested`; nenhum smoke test prova suporte. |
| `status` / `doctor` | Parcial | Contagem de inventário. Ainda não distingue installed, conflict, partial e outras condições de ferramenta. |
| `import` | Parcial | Prévia JSON com hash por recurso; aplicação exige o mesmo plano, confirmação `--yes` e seleção explícita por ID. Origem alterada, plano adulterado, caminho que colide ou destino ocupado é recusado. Skills e prompts Codex têm fixtures de descoberta/importação; publicação de prompts não foi implementada. Importação multi-item pode terminar parcialmente se ocorrer falha de filesystem entre cópias. |
| `add` | Parcial | Preview/apply local/Git com hash, commit pinado, limites de arquivo, cópia auxiliar e proveniência; nomes novos precisam ser portáveis entre Windows/macOS/Linux, e colisões case-insensitive em destinos e registry são recusadas. HTTPS público/local Git somente, sem SSH/auth, identity migração ou smoke remoto. |
| `update` / `remove` / `restore` / `history` | Parcial | Preview/apply aprovado para update, tombstone e restore com snapshot privado; restore exige destino livre e preserva identidade. `history` lista revisão atual e anteriores; remoção de deployment exige plano próprio e mantém snapshot/registro. Faltam histórico de cloud e smoke real. |
| `plan` / `apply` / `rollback` | Parcial | Skills apenas; plan read-only, apply exige `--yes`, cria em destino vazio ou atualiza deployment próprio com snapshot; hash é revalidado. Journal por plano registra `applying/applied/failed/partial`, permite retry de falha pré-write e reconcilia manifesto/hash íntegros após restart. Falha simultânea na troca e restauração fica `partial`, preserva quarentena e pode ser recuperada por plano aprovado com hashes verificados. Remove canônico preserva cópias provider; sem smoke real. |
| `operations` | Parcial | Lista journals e oferece preview + `--plan` + `--yes` para finalizar deployment com hash/revisão confirmados ou restaurar snapshot/quarentena íntegros a destino ausente; root é limitado ao provider/escopo registrado e drift externo não é alterado. Recovery de create/update, falha de troca + rollback e bloqueio de drift cobertos por fixture. |
| Providers | Parcial / not-tested | Codex, Claude Code, OpenCode: caminhos declarados; nenhuma publicação validada em runtime. A auditoria local observou Codex 0.164.0, Claude Code 2.1.296 e OpenCode 2.1.4; nesta sessão Claude Code 2.1.310 e OpenCode 2.1.4 responderam a `--version`. O CLI OpenCode instalado não reconhece `debug skill`; nenhum dos dois passou smoke de carregamento. |
| Schemas / migrations | Parcial | `models` era apenas `Type`, `Root`, `Item`, duplicatas e inventário. Entidades básicas foram adicionadas. `migrate --dry-run` lista candidatos de `~/.agenthub/library` e fingerprint do caminho candidato `agenthub.lock.json`, hashes e conflitos, sem conversão ou escrita. O schema desse lock não foi localizado. |
| State storage | Parcial | Config, registros de deployment e snapshots privados de update/tombstone/remoção; sem fila, transação global ou backup completo. |
| Cloud connect/push/pull/sync/history | Não existe | Nenhum Git/SSH ou staging. Não há evidência de `.git` em `.agents`. |
| Profiles / drift / TUI | Não existe | Sem comando ou serviço correspondente. |
| Testes | Parcial | Cobrem scanner (incluindo distinção prompt/command e bundle de skill), registry, comandos, importação selecionada de skill com arquivos auxiliares e prompt Codex, cópia local, symlink, preview/update/remove/restore/history, listagem/remoção planejada de deployment, journal de sucesso/falha pré-write, reconciliação após restart, recovery de create/update e falha simultânea de troca/rollback, root de destino autorizado, drift pós-preview e aprovação CLI. Não há smoke de provider real, conflito concorrente entre dispositivos ou sync. |

## Validação do checkout antes das alterações

Executados no Windows, sem comandos que escrevam no HOME real; testes existentes usam diretórios temporários para operações de escrita.

| Comando | Resultado |
|---|---|
| `go test ./...` | passou; pacotes `cmd`, `discovery`, `library`, `registry` OK; demais sem testes |
| `go vet ./...` | passou (exit code 0) |
| `go build ./...` | passou (exit code 0) |

## Divergências prioritárias

1. O init deve usar estado privado e ser idempotente, sem adotar `.agents` preexistente.
2. O catálogo deve ser extensível a todos os dez tipos sem alegar publicação correspondente.
3. Metadados AgentHub devem ficar sob `.agents/.agenthub/`; nunca substituir manifestos externos.
4. `import` separa descoberta, prévia e cópia selecionada aprovada; importação multi-item ainda pode falhar parcialmente por erro de filesystem.
5. Providers permanecem `not-tested` para escrita enquanto não houver fixtures fiéis e smoke tests da ferramenta/versionamento. Sessões que possam usar modelos/credenciais devem ser aprovadas e controladas antes da execução.
6. F1 só pode ser aceita quando uma skill percorre import → plan → apply → remove/rollback com dois providers verificados e proteção de arquivos alheios.

## Resultado desta etapa

Foram adicionados tipos de domínio básicos, resolução central dos roots global/privado/projeto, registry sidecar em namespace AgentHub, importação baseada em plano com seleção/hash e prévia de migração legada. Nenhum `~/.agenthub/library` real foi examinado ou migrado; locks legados ficam em `manual_review` e a CLI não oferece aplicação.

## Validação após as alterações

| Comando | Resultado |
|---|---|
| `go test ./...` | passou no Windows; inclui plan/apply/rollback e testes CLI com HOME temporário |
| `go vet ./...` | passou no Windows |
| `go build ./...` | passou no Windows |
| `git diff --check` | passou |
| `GOOS=linux/darwin GOARCH=amd64 go test -c` (pacotes alterados) | compilação dos binários de teste passou; os testes não foram executados nesses sistemas |
| `GOOS=linux/darwin GOARCH=amd64 go build ./...` | passou |

Não foi feito smoke test de provider, teste Git bare, teste de VPS, teste em segundo dispositivo ou teste de execução dos binários em Linux/macOS.
Os testes de deployment simulam raízes nativas temporárias; não provam que as versões listadas carreguem a skill.

## Continuação da migração — importação selecionada

`import` agora retorna plano versionado com candidatos em ordem determinística, hash de cada origem, conflitos de destino e hash global. `--select` escolhe IDs da prévia; a aplicação exige `--plan` e `--yes`, confere biblioteca/provider, integridade do plano e hashes das origens antes de copiar. Cada item ainda é uma cópia independente; falha de filesystem no meio de vários itens pode produzir resultado parcial, informado no JSON e no código de saída.

Nesta atualização, `go test ./cmd -run TestImport -count=1`, `go test ./...`, `go vet ./...`, `go build ./...`, builds `GOOS=linux/darwin GOARCH=amd64` e `git diff --check` passaram. O help local do OpenCode 2.1.4 não lista `debug skill`, apesar de a documentação atual listar esse comando; o CLI de Claude não oferece comando equivalente documentado sem sessão. Nenhum smoke de carregamento foi executado, então publicação continua `not-tested`.

## Continuação da migração — prévia de dados legados

`agenthub migrate --dry-run` mapeia diretórios por tipo/nome de `~/.agenthub/library` para `~/.agents`, calcula hashes, distingue itens `ready`, `already_present` e conflitos, detecta nomes case-insensitive e entradas já registradas sem conteúdo. O caminho candidato `agenthub.lock.json` é apenas fingerprinted para revisão manual; não há schema conhecido, parse ou conversão. O comando não tem modo de aplicação nem cria raízes. A execução padrão read-only nesta máquina retornou `source_present=false`, zero candidatos e lock legado ausente; nenhum dado foi copiado ou alterado.

Testes em `t.TempDir()` cobrem candidato pronto, conteúdo divergente, registro sem diretório, colisão de caixa, lock legado, roots ausentes/sobrepostos e ausência de escrita.

## Continuação F1a — prompts Codex

O scanner confundia `.codex/prompts/*.md` com `command`, apesar de a raiz Codex declarar `prompts`. A classificação agora preserva `models.Prompt`; `import --from codex` produz plano do tipo `prompts` e copia somente após selecionar e aprovar o candidato. Testes cobrem classificação distinta de `commands`, hash-bound preview, conteúdo copiado intacto e ausência de escrita em `commands`. Isso não valida publicação/execução em Codex nem altera configurações nativas.

## Continuação F1a — bundles de skills

O scanner tratava cada Markdown dentro de `skills/` como skill separada, embora `import` copie a pasta da skill identificada por `SKILL.md`. Agora somente arquivos `SKILL.md` são candidatos; README e referências ficam dentro do bundle selecionado. Fixtures cobrem descoberta e importação preservando esses arquivos auxiliares. Nenhum provider real foi executado.

Validação desta alteração: `go test ./...`, `go vet ./...`, `go build ./...` e `git diff --check` passaram no Windows. O diff check exibiu apenas avisos Git sobre conversão LF/CRLF dos arquivos Go já modificados. Sem smoke real ou cross-build nesta etapa.

## Continuação F1a — catálogo `list`

`list` repetia `scan` e não mostrava recursos já importados para `.agents`. Agora consulta somente o registry canônico e identifica escopos global/projeto; `scan` continua sendo a consulta de descoberta nativa. Fixture verifica os dois escopos e garante que um recurso descoberto mas não importado não apareça no catálogo. `find` continua pendente.

`show <resource-id>` permite revisar registro, origem e revisão atual; IDs distintos permanecem independentes entre escopos.

`find <query>` busca sem distinção de caixa por ID, slug, tipo, escopo, origem e proveniência registrada. Não lê nem imprime conteúdo de arquivos; busca textual de documentos `knowledge` permanece pendente. Fixture cobre correspondência metadata e a exclusão de conteúdo.

Validação: `go test ./...`, `go vet ./...`, `go build ./...` e `git diff --check` passaram no Windows. Nenhuma escrita em HOME ou provider real.

Validação desta atualização: `go test ./...`, `go vet ./...`, `go build ./...` e `git diff --check` passaram no Windows. Não foram executados providers reais nem alterados dados de HOME.

## Verificação da fonte histórica do lock

O checkout arquivado `Projetos/Arquivados/harness-library` contém especificações e scaffold greenfield; `docs/F0-AUDIT.md` declara que o código antigo não existe localmente e que não haverá recuperação/migração desse código. A arquitetura menciona `~/.harness/library` como árvore materializada e PostgreSQL para metadados, mas não fornece formato de `agenthub.lock.json` nem implementação local. Uma busca de texto pelo nome do lock no checkout arquivado e no AgentHub arquivado não encontrou definição. Portanto, a prévia atual é somente inventário de uma árvore passada pelo operador e fingerprint de um path conhecido; não é conversor de Harness Library.

## Continuação F1a — paths portáveis

`add` agora recusa nomes de recursos inválidos no Windows (caracteres reservados, dispositivos como `CON`/`NUL`, `.git` e controles), verifica nomes vizinhos e entradas do registry sem distinguir caixa e repete a checagem antes da gravação. `migrate --dry-run` marca nomes não portáveis como `blocked`. Fixtures cobrem nomes inválidos, collision em registry tombstoned, comparação Unicode case-insensitive e round-trip de bundle com diretórios/arquivos Unicode e espaços.

Validação da etapa de nomes e Unicode: `go test ./...`, `go vet ./...`, `go build ./...`, builds `GOOS=linux/darwin GOARCH=amd64 go build ./...` e `git diff --check` passaram no Windows. Os builds cross-OS confirmam compilação, não execução nesses sistemas. Junctions foram testadas separadamente no Windows; execução em Linux/macOS segue pendente.

## Continuação F1a — junctions Windows

Um teste real com `New-Item -ItemType Junction` mostrou que o Go usado aqui sinaliza junction como `ModeIrregular`, não `ModeSymlink`; as proteções anteriores deixavam o caminho passar. `internal/fsutil` agora reconhece symlinks e junctions, e library/deployment usam a checagem comum. A fixture verifica rejeição em caminhos ancestrais e dentro de bundles em import, hash e cópia. O teste de junction passou no Windows nesta sessão.

Validação após a correção: `go test ./...`, `go vet ./...`, `go build ./...`, builds `GOOS=linux/darwin GOARCH=amd64 go build ./...` e `git diff --check` passaram. Apenas a execução de runtime Windows foi realizada.

## Continuação F1a — IDs estáveis e rename canônico

`Entry.ID` era derivado do nome, então rename não podia preservar identidade. Novos recursos agora recebem UUIDs, sem reescrever IDs de registros existentes. `agenthub rename <nome> --type <tipo> --to <novo>` mostra plano JSON; a aplicação exige plano salvo e `--yes`, revalida ID/path/hash e move apenas o conteúdo canônico. O ID e a revisão permanecem iguais; deployments em providers não são alterados. O journal de deployment aceita IDs UUID e os IDs legados `skills:<slug>`. Fixtures cobrem rename aprovado, edição pós-preview bloqueada, preservação do ID legado e fluxo CLI. `go test ./...`, `go vet ./...`, `go build ./...` e `git diff --check` passaram; builds cross-OS não foram repetidos nesta etapa.

Nova tentativa de smoke OpenCode com `opencode v2.1.4`, skill fixture em `.agents/skills/agenthub-smoke` e HOME/XDG/config/data/cache/state isolados confirmou que `opencode debug skill` não existe no binário instalado (exit 1, unknown subcommand). Nenhuma sessão ou prompt de modelo foi iniciado. A documentação oficial atual descreve esse comando e a descoberta compartilhada `.agents/skills`; a discrepância impede confirmar consumo real nessa versão. Claude Code continua sem caminho de listagem local documentado que evite sessão.

## Continuação F1a — busca textual local em knowledge

`find` agora percorre somente recursos `knowledge` do registry, valida os bundles com as proteções de arquivos/links e pesquisa texto em blocos nos formatos `.md`, `.markdown`, `.txt`, `.text`, `.rst`, `.adoc`, `.csv`, `.json`, `.yaml`, `.yml` e `.toml`. A comparação ignora caixa; imagens e formatos não listados são ignorados. O resultado contém somente o registro do recurso, sem caminho do arquivo, trecho ou conteúdo. Fixtures verificam UTF-8, correspondência entre blocos, exclusão de binários, busca metadata mantida e ausência de conteúdo na saída.

O adapter Claude Code consultava `os.UserHomeDir()` internamente e ignorava a raiz injetada. Agora usa o parâmetro `home`; fixture garante que todos os roots globais permaneçam sob a raiz temporária. A CLI local não oferece listagem standalone: `/skills` é comando de sessão segundo a referência oficial. Como o launcher local injeta um modelo no binário, não foi iniciado; runtime permanece `not-tested`.

## Continuação F1a — publicação de destinos sem sobrescrita

Uma condição de corrida permitia que um destino vazio criado após a validação fosse substituído por `os.Rename` durante o commit de conteúdo. `fsutil.RenameNoReplace` agora usa primitiva nativa Linux, Windows e macOS; os fluxos de biblioteca e deployment que exigem destino ausente usam essa operação. Atualização intencional de `deployment.json` e arquivos de registry/journal seguem com rename atômico substitutivo. Em outros sistemas, a publicação falha explicitamente em vez de arriscar sobrescrita. Fixtures verificam preservação do diretório vazio concorrente e falha de Apply sem alterá-lo.

Validação: `go test ./...`, `go vet ./...`, `go build ./...`, builds `GOOS=linux/darwin GOARCH=amd64 go build ./...` e `git diff --check` passaram no Windows. Linux/macOS tiveram compilação cruzada, sem validação de runtime. O pacote `golang.org/x/sys` foi incluído para `renameat2` em Linux.

## Reforço do aceite FR-01 — `init` preserva `.agents`

O teste de `init` agora cria `~/.agents/agents.lock.json` com bytes externos conhecidos, executa `init` duas vezes e confirma que o lock e a configuração já existente do AgentHub permanecem byte a byte iguais. Validação focada: `go test ./cmd -run '^TestInitCreatesOnlyAgentHubConfig$' -count=1` e `git diff --check` passaram no Windows. Nenhum home real foi alterado.

## F2 inicial — reconciliação 3-way por resourceId

`internal/cloud.Reconcile` compara base, local e remoto por conteúdo/tombstone, ignora timestamps e ordena decisões por `resourceId`. Mudança apenas local resulta em `publish-local`; apenas remota, `materialize-remote`; conteúdos iguais convergem; mudanças divergentes preservam as três revisões em `conflict`. Recurso rastreado ausente sem tombstone explícito também conflita, evitando interpretar perda de metadados como exclusão. Testes cobrem esses casos e rejeição de revisão viva sem hash.

Esta etapa ainda não implementa persistência, staging Git, CLI, filtro de secrets, fila offline ou push; o reconciliador está pronto para ser integrado ao executor da F2. Validação focada: `go test ./internal/cloud -count=1`, `go vet ./internal/cloud` e `git diff --check` passaram no Windows.

## F2 — snapshot isolado para recursos aprovados

`cloud.BuildSnapshot` materializa staging fora da raiz canônica e exige seleção explícita de resourceIds registrados. Copia apenas entradas ativas selecionadas, valida o hash do conteúdo contra o registry, valida paths/names portáveis, rejeita links e arquivos especiais, aplica limites de tamanho/quantidade e escreve manifesto sem origem/path local. Tombstones selecionados entram no manifesto sem reexportar o conteúdo removido. Nomes/extensões sensíveis e padrões comuns de credenciais bloqueiam o snapshot; referências `${ENV}` são aceitas. O destino precisa não existir e a publicação usa rename sem substituição.

A etapa ainda não inicializa Git, não possui CLI de aprovação e não publica nem materializa remotamente. A heurística de credenciais pode ter falsos negativos; revisão humana permanece necessária. Testes locais cobrem seleção, drift, secret literal, referência de ambiente, tombstone e destino preexistente.

## F2 — transporte Git local inicial

`InitBareRepository`, `CloneBareRepository`, `CommitSnapshot` e `PushSnapshot` adicionam o primeiro ciclo local em repositórios bare temporários. Commits são feitos somente em staging que corresponde exatamente ao manifesto retornado pela seleção aprovada, com hashes revalidados; push usa refspec normal em `main`, sem force. Configuração Git local é allowlisted, hooks ativos no remoto são recusados, hooks locais são desativados por comando e protocolos ficam limitados a `file`. `.gitattributes` é bloqueado para impedir filtros e normalização não controlados dos bytes.

Teste local: snapshot foi commitado, enviado a bare Git, clonado como bare e conferido pelo hash do commit; retry do mesmo commit é idempotente. Testes também recusam Git sob `.agents`, destino preexistente, filtro Git local e hook remoto ativo. O suporte SSH, checkout/materialização, atualização do worktree, pull/reconciliação CLI, fila offline e corrida entre clientes seguem pendentes.

## F2 — recebimento validado como candidato isolado

`cloud.FetchSnapshot` lê somente `refs/heads/main` de um bare Git local e extrai o archive para uma pasta temporária fora de `.agents`. O parser aceita arquivos/diretórios regulares, valida paths portáveis e case collisions, rejeita links/entradas especiais/traversal, limita entradas/bytes, valida manifesto canônico, hashes e heurística de segredos e publica o destino sem substituição. O resultado não é aplicado na biblioteca. O leitor aceita o PAX global `comment` emitido pelo Git apenas quando contém um hash hexadecimal de commit. Fixture confirma round-trip de conteúdo e rejeita path traversal e symlink.

Em Windows, a primeira execução revelou que `tar.Reader` pode parar antes do EOF físico do pipe e que um processo filho mantinha stderr herdado aberto; o leitor agora drena stdout e redireciona stderr para arquivo temporário antes de aguardar Git. Teste focado `go test ./internal/cloud -count=1 -timeout=45s` passou.

## F2 — plano integrado local/base/remoto

`cloud.PlanSync` converte snapshots versionados em estados por resourceId e chama o reconciliador 3-way. A decisão preserva base/local/remoto e a versão escolhida para materialização ou publicação; valida resourceIds, hashes, tipos, nomes e colisões de paths. Renames contam como mudança mesmo quando o hash dos bytes não muda; duas renomeações divergentes conflitam, e mudança de tipo sob o mesmo resourceId é rejeitada. `VerifySyncPlan` confere o fingerprint do JSON ordenado do plano (integridade, não assinatura nem autorização). Fixtures cobrem rename unilateral, rename concorrente, conflito com bytes diferentes, duplicação e adulteração do plano.

Ainda não há aprovação persistida nem executor das decisões; o plano é somente preview e não escreve na biblioteca nem no remoto.

## F2 — aplicação de uma decisão remota aprovada (parcial)

`cloud.ApplyRemoteDecision` aplica uma decisão `materialize-remote` após verificar integridade do plano, manifesto/conteúdo do candidato e estado local contra o snapshot aprovado. Add/update preservam `resourceId` e `revisionId` remotos; update salva a revisão anterior em snapshot privado. Replay idempotente é aceito sem criar revisão extra. Tombstone remoto só é aplicado quando identidade, caminho e hash local coincidem; rename, restauração de tombstone local e exclusão divergente permanecem bloqueados para resolução manual.

Validação focada: `go test ./internal/cloud ./internal/library -count=1` passou no Windows. Isto não implementa fluxo de aprovação persistida/CLI, sync completo, concorrência entre dispositivos, SSH ou fila offline.
