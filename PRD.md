# AgentHub — Product Requirements Document (PRD)

> **Versão:** 0.1.0 (baseline de produto)  
> **Status:** especificação-alvo; NÃO significa funcionalidades já implementadas  
> **Data:** 2026-10-24  
> **Produto:** AgentHub · **CLI:** `agenthub` · **Biblioteca:** `~/.agents` · **Estado privado:** `~/.agenthub`  
> **Documentos:** [ARCHITECTURE.md](./ARCHITECTURE.md) · [AGENTS.md](./AGENTS.md)

## 1. Visão e posicionamento

**AgentHub é uma biblioteca universal, local-first e self-hosted de recursos para ferramentas/agentes de IA, administrada por uma CLI Go e sincronizável entre dispositivos por uma VPS privada.** A visão e o domínio vêm do projeto **Harness Library**; as práticas de configuração canônica, importação e publicação para múltiplos providers têm o **AgentLoom** como referência conceitual.

**Visão:** uma biblioteca pessoal, muitos agentes, nenhum aprisionamento por provider.

Não é apenas um sincronizador de skills nem um executor de LLMs. Diferenciar sempre:

1. **Library:** criação, catálogo, importação, proveniência, versão, exclusão/restauração e busca.
2. **Cloud Sync:** replicar o conteúdo canônico aprovado entre máquinas (VPS ↔ `~/.agents`).
3. **Apply/Adapters:** converter/materializar conteúdo da biblioteca nos diretórios e formatos aceitos por cada ferramenta.

O projeto atual é **CLI-first**: sem web, API dedicada ou serviço residente obrigatório nas fases F0–F3. Essas possibilidades permanecem como evolução, não serão simuladas como prontas.

## 2. Problema e público

Configurações de Codex, Claude Code, Gemini CLI, OpenCode, Hermes Agent e outras ferramentas ficam espalhadas por diretórios globais e projetos. Skills, prompts, memória explícita, regras, MCPs, plugins e documentos acabam duplicados, desatualizados e sem origem/histórico comum. A migração entre Windows, Linux e macOS é manual e arriscada.

**Usuário-alvo inicial:** desenvolvedor individual com múltiplas CLIs de IA, mais de um computador e uma VPS privada. Single-user no início. O AgentHub deve continuar útil completamente offline; não requer conta SaaS.

## 3. Objetivos e medidas verificáveis

| ID | Objetivo | Critério de sucesso |
|---|---|---|
| G1 | Inventário seguro | `scan` identifica recursos e providers sem modificar arquivos nem executar scripts |
| G2 | Biblioteca universal | Gerenciar as categorias do domínio na biblioteca `~/.agents`, com identidade, escopo e origem |
| G3 | Interoperabilidade | Uma skill validada funciona em pelo menos dois providers suportados, preservando arquivos preexistentes |
| G4 | Instalação segura | `plan` mostra diffs; `apply` exige precondições, backup e registra deployment |
| G5 | Sincronização privada | Duas máquinas trocam recursos gerenciados por VPS Git/SSH sem forçar merge nem apagar divergências |
| G6 | Offline-first | Criar/editar recursos e enfileirar alterações sem rede; retomar depois sem perder revisão-base |
| G7 | Recuperação | Histórico, lixeira com tombstone, rollback local e restore de backup remoto ensaiado |
| G8 | Configuração por contexto | Perfis globais/projeto e políticas de publicação coerentes por provider |
| G9 | Transparência | `status`/`doctor` distinguem instalado, testado, parcial, conflitante e não suportado |

## 4. Princípios inegociáveis

- **`.agents` como única fonte canônica de conteúdo portátil.** `~/.agenthub` contém somente estado interno, snapshots/espelhos derivados, cache, logs e configuração específica do dispositivo.
- **Biblioteca compartilhada, não exclusiva:** o AgentHub não é proprietário de toda a árvore `.agents`; nunca sobrescreve conteúdo externo sem consentimento e rastreamento de posse.
- **Library-first e offline-first:** versão/identidade do recurso independem do diretório nativo de um provider ou de disponibilidade da VPS.
- **Conservador por padrão:** scan read-only; importação com aprovação; prévia antes de writes; rollback viável.
- **Capacidade verificada:** presença de pasta não prova que a CLI consumirá o recurso; usar probes, matriz por versão e testes reais.
- **Segredos fora da biblioteca:** referências (`${TOKEN}`, por exemplo) podem ser portáveis; valores, tokens e chaves nunca são incluídos no Git ou em logs.
- **Sem execução implícita:** baixar ou copiar uma skill/plugin não autoriza executar seus scripts, hooks ou servidores MCP.
- **Separação de responsabilidades:** Cloud Sync e Apply não se acionam automaticamente mutuamente na baseline.

## 5. Catálogo universal de recursos

| Tipo (`kind`) | Representação canônica preferida | Comportamento inicial |
|---|---|---|
| `agent` | Markdown + frontmatter/manifesto | Catalogar/importar; publicar onde suporte real for demonstrado |
| `skill` | `skills/<slug>/SKILL.md` + `references/`, `scripts/`, `assets/` | Fluxo end-to-end prioritário (importar, aplicar, sincronizar, restaurar) |
| `prompt` | `prompts/<slug>.md` ou bundle versionado | Catalogar, editar e versionar; adaptar quando compatível |
| `command` | `commands/<slug>.md` e metadados | Conversão por provider validado; não confundir com prompt livre |
| `rule` | `rules/<slug>.md` | Gerar blocos gerenciados em instruções/config nativas, com merge controlado |
| `memory` | `memory/<slug>.md` + escopo, proveniência, aprovação | **Memória declarada/proposta**, nunca memória interna inacessível de provedores cloud |
| `mcp` | `mcp.json` (declaração) + `mcps/` (metadados opcionais) | Instalar apenas declarações sem segredos, conforme compatibilidade/permissões |
| `plugin` | `plugins/<slug>/` + manifesto/artefatos | Catálogo e validação; instalação executável posterior, mediante consentimento |
| `knowledge` | `knowledge/<slug>/` + documentos permitidos | Catálogo e busca textual; não pressupor RAG/vetores |
| `profile` | `profiles/<slug>.yaml` | Conjunto de recursos/providers habilitados e overrides de escopo |

**Compatibilidade não é universal:** cada tipo pode ser `supported`, `partial`, `unsupported`, `not-tested` ou `permission-required` para cada provider/versão. Recursos sem conversão validada continuam catalogáveis.

### Identidade e origem

Cada recurso gerenciado tem `resourceId` estável, `kind`, `slug`, `scope`, origem, conteúdo/hash, revisão-base, dependências opcionais, confiança, status e lista de deployments. Renomear um caminho não cria nova identidade. Versões confirmadas são imutáveis. Metadados portáveis vivem em `~/.agents/.agenthub/`; estado por máquina fica apenas em `~/.agenthub/`.

## 6. Escopos e precedência

- **Global:** `~/.agents/` para recursos compartilhados pela conta local.
- **Projeto:** `<repo>/.agents/`, apenas quando projeto foi autorizado/configurado. Projeto tem ID estável, independente de nome da pasta.
- A seleção do AgentHub usa **projeto > global** para mesmo `kind + slug`; a publicação obedece à precedência e à capacidade real da ferramenta, verificadas pelo adaptador.
- Não escanear discos inteiros por padrão; escopos e raízes de busca precisam ser consentidos.

## 7. Jornada de uso e experiência CLI

### J1 — Primeiro uso seguro

1. Instalar binário Go; executar `agenthub init` (idempotente, preserva `.agents` existente).
2. Executar `agenthub scan --json` e `agenthub doctor`; localizar providers, caminhos, possíveis recursos e problemas.
3. Executar `agenthub import --from codex --dry-run` e selecionar o que será importado.
4. Revisar `agenthub list`, `agenthub show <id>` e proveniência registrada.

### J2 — Instalar uma skill

1. `agenthub add github:owner/repo --dry-run` ou `agenthub add ./skill`.
2. Selecionar conteúdo e revisar riscos/permissões, origem Git/ref/hash e possíveis colisões.
3. Aprovar importação canônica; `agenthub plan --provider codex,claude`.
4. `agenthub apply --plan <id>` após validação do plano; confirmar descoberta funcional quando possível.
5. `agenthub deployments` mostra destinos, hashes e status.

### J3 — Sincronizar máquinas

1. Preparar repositório Git bare privado na VPS; `agenthub cloud connect <ssh-url>`.
2. `agenthub cloud sync` publica/recebe **somente conteúdo canônico permitido**, com revisão-base.
3. Outra máquina recebe o mesmo conteúdo; `agenthub plan` e `agenthub apply` materializam apenas onde adequado.
4. Se ambas editarem o mesmo recurso, bloqueia a aplicação conflitante até resolver.

### J4 — Alterações fora do AgentHub

1. Uma ferramenta altera uma skill gerenciada no destino nativo.
2. `agenthub drift` identifica hash divergente.
3. Usuário escolhe importar a edição, reaplicar a biblioteca, preservar ambos ou ignorar. Não sobrescrever silenciosamente.

### J5 — Memória proposta

Um agente/usuário registra uma memória como `pending` com escopo, origem e motivo; apenas após aprovação passa a `approved`. Conteúdo privado/sensível não entra automaticamente na nuvem ou em outros providers; consentimento e classificação são obrigatórios.

### J6 — Excluir e restaurar

Excluir recurso gera tombstone/lixeira com retenção configurável (padrão proposto: 30 dias). Excluir deployments exige plano e confirmação própria; `restore` cria revisão nova, sem alterar revisões antigas.

## 8. Comandos e semântica pretendida

| Grupo | Comandos | Semântica |
|---|---|---|
| Setup | `init`, `version`, `doctor`, `status` | Preparação e diagnóstico |
| Discovery | `scan`, `providers`, `list`, `show`, `find` | Somente leitura |
| Biblioteca | `import`, `add`, `update`, `remove`, `restore`, `history` | Mudanças canônicas e versão |
| Publicação | `plan`, `apply`, `deployments`, `drift`, `rollback` | Materialização e posse nos providers |
| Perfis | `profile list/create/use/show/delete` | Seleção e overrides portáveis |
| Nuvem | `cloud connect/status/push/pull/sync/history/disconnect` | Réplica canônica via Git/SSH |
| Interação | `ui` | TUI baseada no core; não substitui CLI |
| Futuro | `watch`, `pause`, `resume`, `vault`, `devices`, `serve` | Não prometer antes de implementados |

Contrato comum: `--json` para operações de consulta; logs no `stderr`; `--dry-run` em todas as mutações pertinentes; `--global`/`--local`; `--provider`; `--yes` **não** deve contornar aprovações de risco alto nem conflitos. `agenthub sync` só existirá como alias explícito de `agenthub cloud sync` se introduzido e documentado, nunca como substituto silencioso de `apply`.

## 9. Requisitos funcionais e prioridades

| ID | Prioridade | Requisito | Aceite verificável |
|---|---|---|---|
| FR-01 | P0 | `init` idempotente respeita conteúdo `.agents` existente | Dois `init` não alteram hashes anteriores |
| FR-02 | P0 | Descoberta segura e inventário por provider | Scan com fixtures Windows/macOS/Linux |
| FR-03 | P0 | Catálogo dos tipos de recurso | Lista/proveniência/escopo com schemas versionados |
| FR-04 | P0 | Importação local/Git com prévia | Sem source script execution; lock/source/hash corretos |
| FR-05 | P0 | Skill end-to-end | Importar → planejar → aplicar → atualizar → remover → rollback |
| FR-06 | P0 | Ownership de deployments | Sem exclusão/escrita em arquivo sem posse ou aprovação |
| FR-07 | P0 | Backups e lixeira | Recuperação íntegra inclusive após erro parcial |
| FR-08 | P0 | Cloud Sync Git/SSH com comparação de base | Convergência de duas máquinas e conflito preservado |
| FR-09 | P0 | Offline/pendências duráveis | Retentar após reinício sem perda de estado |
| FR-10 | P0 | Exclusão de segredos do sync | Testes de paths e payloads sensíveis barrados |
| FR-11 | P1 | Perfis por contexto | Alternância não apaga recursos fora do perfil |
| FR-12 | P1 | Drift/diff e resolução explícita | Conflitos jamais sobrescritos sem decisão |
| FR-13 | P1 | TUI funcional | Reutiliza casos de uso CLI e exige confirmações |
| FR-14 | P1 | Adapters extras Gemini/Hermes/Pi/Cursor/Copilot | Cada suporte baseado em probe real e fixtures |
| FR-15 | P1 | Memórias com aprovação e permissões | Nenhuma proposta pendente distribuída |
| FR-16 | P1 | MCP declarativo com referências a secrets | Não grava valores de tokens em formatos remotos |
| FR-17 | P2 | Plugins executáveis e dependências | Análise de risco, sandbox/consentimento e rollback |
| FR-18 | P2 | Knowledge/RAG avançado | Busca semântica somente após necessidade medida |
| FR-19 | Futuro | API + dashboard | PRD/ADR próprios antes de implementar |
| FR-20 | Futuro | Vault E2E + pareamento de dispositivos | Desbloquear somente após criptografia/recovery testados |

## 10. Roadmap (escopo atual e evolução)

| Fase | Objetivo | Saída mínima |
|---|---|---|
| **F0 — Auditoria do existente** | Inspecionar projeto Go real e inventário; testes; registrar gaps | Relatório do estado atual, sem migração destrutiva |
| **F1 — Library & local management** | Schemas dos tipos, biblioteca, importação, plan/apply, manifestos, backup | Skill funcional em pelo menos dois providers verificados |
| **F2 — VPS & cloud sync** | Git bare SSH, sync 3-way, fila offline, bloqueio de segredos | Dois dispositivos simulados, alterações/colisões testadas |
| **F3 — Advanced CLI** | Perfis, drift, conflitos, TUI, extensibilidade e diagnósticos | Core reutilizado, UX funcional, nenhuma regressão F1/F2 |
| **F4 — Hardening** | Segurança reforçada, backups/restore remotos, distribuição assinável | Testes de falha e release multiplataforma |
| **F5 — Plataforma futura** | Avaliar API, dashboard, vault E2E, devices e automação | Decisões em ADRs próprias; sem compromisso de MVP |

**Importante:** nomenclatura F1/F2/F3 aqui substitui a do Harness Library antigo; não misturar os cronogramas.

## 11. Requisitos não funcionais

- **Portabilidade:** Go; compilação Windows, Linux e macOS; Unicode, espaços, separadores, case-collision, symlinks/junctions.
- **Confiabilidade:** writes atômicos quando suportados, verificação pós-write, snapshots antes de atualização e retornos de erro precisos.
- **Performance:** scan seletivo e incremental, limites para arquivos grandes; sem daemon obrigatório.
- **Privacidade:** sem telemetria externa por padrão; logs com redaction; não executar arquivos importados.
- **Observabilidade local:** log estruturado, ID de operação, relatórios legíveis e JSON.
- **Dependências:** preferir Go stdlib, Cobra para CLI, Bubble Tea/Lip Gloss somente na F3; estado SQLite puro-Go se necessário, sem bloquear melhorias futuras.
- **VPS:** acesso SSH, usuário de privilégios mínimos, sem banco/API exigidos na F2; Git remoto NÃO fornece criptografia de conteúdo em repouso.

## 12. Fora do escopo imediato

Dashboard, API pública, autenticação multiusuário, execução de modelos ou automação de agentes, instalação irrestrita das próprias ferramentas de IA, marketplace, monitoramento de conversas cloud, inferência de memória privada, deploy de plugins desconhecidos, RAG semântico obrigatório e cofre E2E não validado.

## 13. Dependências, riscos e decisões abertas

| Tema | Decisão provisória/mitigação |
|---|---|
| `.agents` compartilhada | Posse explícita por arquivo/deployment; desconhecidos protegidos |
| Outros projetos usam `.agents` | Adaptadores compatíveis, não alterar arquivos de lock/manifest do AgentLoom |
| Metadata da biblioteca | Namespace `~/.agents/.agenthub/` reservado ao AgentHub, sem alterar `SKILL.md` de terceiros |
| Git remoto vê recursos em claro | Não sincronizar segredos; dados privados exigem decisão adicional de criptografia |
| Recursos grandes | Limites + exclusão por padrão, em vez de commitar binários inesperados |
| Estado local | SQLite/JSON operacional reconstruível, nunca único repositório de conteúdos |
| Suporte por provider | Capabilities observadas e testes específicos por versão |
| Edição de conteúdo nativo | Drift e reconciliação aprovada; sem auto-import de recurso desconhecido |
| Vault futuro | Exigirá E2E real com chaves fora da VPS, pairing/recovery e revisão de segurança |

## 14. Aceitação do MVP de CLI

- [ ] Scanner não altera bytes de diretórios encontrados.
- [ ] Conteúdo preexistente em `.agents` é preservado, inclusive arquivos de outros gerenciadores.
- [ ] Skill é importada com arquivos auxiliares, origem/hashes e ID estável.
- [x] Rename/move canônico preserva resourceId e revisões, mantendo deployments externos intactos até ação explícita.
- [ ] `plan/apply` funcional com ao menos Codex + Claude ou outro par realmente validado.
- [ ] Mudanças concorrentes em origem canônica/destino nativo são detectadas.
- [ ] Two-client Git bare test: sucesso sem conflito, conflito com preservação, offline retry.
- [ ] Dados sensíveis não são enviados a repositório remoto.
- [ ] Rollback local e restauração da lixeira foram testados.
- [ ] Perfis/TUI não duplicam lógica e não escondem conflitos.
- [ ] `doctor` separa `detected`, `supported`, `partial`, `not-tested`, `unsupported`, `conflict`, `error`.

## 15. Referências

- Documentos originais fornecidos: Harness Library `PRD.md`, `ARCHITECTURE.md`, `AGENTS.md` (2026-10-24).
- AgentLoom: https://agentloom.sh/docs · https://github.com/farnoodma/agentloom
- Referências de providers devem ser revistas junto com versão instalada, antes de habilitar escrita.
