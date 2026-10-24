package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ThiagoAciole/agenthub/internal/deployment"
	"github.com/ThiagoAciole/agenthub/internal/discovery"
	"github.com/ThiagoAciole/agenthub/internal/library"
	"github.com/ThiagoAciole/agenthub/internal/models"
	"github.com/ThiagoAciole/agenthub/internal/platform"
	"github.com/ThiagoAciole/agenthub/internal/providers"
	"github.com/ThiagoAciole/agenthub/internal/registry"
	"github.com/spf13/cobra"
)

type options struct {
	json    bool
	scope   string
	project string
}

func NewRootCommand(out, errOut io.Writer) *cobra.Command {
	opts := &options{}
	root := &cobra.Command{Use: "agenthub", Short: "Inventário local de agentes e ferramentas de IA", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(out)
	root.SetErr(errOut)
	root.PersistentFlags().BoolVar(&opts.json, "json", false, "Emitir resultado em JSON")
	root.PersistentFlags().StringVar(&opts.scope, "scope", "all", "Escopo: all, global ou project")
	root.PersistentFlags().StringVar(&opts.project, "project", "", "Diretório do projeto (padrão: diretório atual)")
	root.AddCommand(newInit(opts), newScan(opts), newList(opts), newShow(opts), newFind(opts), newProviders(opts), newStatus(opts), newDoctor(opts), newAdd(opts), newImport(opts), newMigration(opts), newRename(opts), newUpdate(opts), newRemove(opts), newRestore(opts), newHistory(opts), newDeployments(opts), newOperations(opts), newPlan(opts), newApply(opts), newRollback(opts))
	return root
}

func newRename(opts *options) *cobra.Command {
	var typ, newName, planPath string
	var approve bool
	cmd := &cobra.Command{Use: "rename <current-name>", Short: "Planeja ou renomeia um recurso canônico sem mudar sua identidade", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if opts.scope != "global" && opts.scope != "project" {
			return fmt.Errorf("rename exige --scope global ou project")
		}
		root, err := resolveLibraryRoot(opts)
		if err != nil {
			return err
		}
		if planPath == "" {
			if typ == "" || newName == "" || approve {
				return fmt.Errorf("prévia: rename <nome-atual> --type <tipo> --to <novo-nome>; depois use --plan <arquivo> --yes")
			}
			plan, err := library.PlanRename(root, args[0], typ, newName)
			if err != nil {
				return err
			}
			return output(cmd, &options{json: true}, plan)
		}
		if !approve || newName != "" || typ != "" {
			return fmt.Errorf("rename com --plan exige --yes e não aceita --type/--to")
		}
		b, err := readPlanFile(planPath)
		if err != nil {
			return err
		}
		var plan library.RenamePlan
		if err := json.Unmarshal(b, &plan); err != nil {
			return fmt.Errorf("plano JSON inválido: %w", err)
		}
		if plan.Root != root || plan.From != args[0] {
			return fmt.Errorf("plano pertence a outro recurso/biblioteca")
		}
		entry, err := library.ApplyRename(plan, root)
		if err != nil {
			return err
		}
		return output(cmd, &options{json: true}, entry)
	}}
	cmd.Flags().StringVar(&typ, "type", "", "Tipo do recurso")
	cmd.Flags().StringVar(&newName, "to", "", "Novo nome do recurso")
	cmd.Flags().StringVar(&planPath, "plan", "", "Aplica um plano JSON revisado")
	cmd.Flags().BoolVar(&approve, "yes", false, "Aprova o rename do recurso canônico")
	return cmd
}

func newUpdate(opts *options) *cobra.Command {
	var typ, planPath string
	var approve bool
	cmd := &cobra.Command{Use: "update [<resource-name> <local-resource>]", Short: "Planeja ou atualiza um recurso canônico com snapshot", Args: cobra.RangeArgs(0, 2), RunE: func(cmd *cobra.Command, args []string) error {
		if opts.scope != "global" && opts.scope != "project" {
			return fmt.Errorf("update exige --scope global ou project")
		}
		libraryRoot, err := resolveLibraryRoot(opts)
		if err != nil {
			return err
		}
		if planPath == "" {
			if len(args) != 2 || typ == "" {
				return fmt.Errorf("prévia: update <nome> <origem> --type <tipo>; depois use --plan <arquivo> --yes")
			}
			if approve {
				return fmt.Errorf("gere e revise o plano primeiro; --yes só pode ser usado com --plan")
			}
			plan, err := library.PlanUpdate(libraryRoot, args[0], args[1], typ)
			if err != nil {
				return err
			}
			return output(cmd, &options{json: true}, plan)
		}
		if len(args) != 0 {
			return fmt.Errorf("não combine argumentos posicionais com --plan")
		}
		if !approve {
			return fmt.Errorf("update exige aprovação explícita com --yes")
		}
		planFile, err := os.Open(planPath)
		if err != nil {
			return err
		}
		defer planFile.Close()
		info, err := planFile.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return fmt.Errorf("plano deve ser arquivo regular de até 1 MiB")
		}
		b, err := io.ReadAll(io.LimitReader(planFile, (1<<20)+1))
		if err != nil {
			return err
		}
		if len(b) > 1<<20 {
			return fmt.Errorf("plano excede 1 MiB")
		}
		var plan library.UpdatePlan
		if err := json.Unmarshal(b, &plan); err != nil {
			return fmt.Errorf("plano JSON inválido: %w", err)
		}
		if plan.Root != libraryRoot {
			return fmt.Errorf("plano pertence a outra biblioteca/escopo")
		}
		paths, err := platform.New().Resolve(opts.project)
		if err != nil {
			return err
		}
		entry, err := library.ApplyUpdate(plan, libraryRoot, paths.State)
		if err != nil {
			return err
		}
		return output(cmd, &options{json: true}, entry)
	}}
	cmd.Flags().StringVar(&typ, "type", "", "Tipo do recurso, por exemplo skills")
	cmd.Flags().StringVar(&planPath, "plan", "", "Aplica um plano JSON já revisado")
	cmd.Flags().BoolVar(&approve, "yes", false, "Aprova a atualização do recurso canônico")
	return cmd
}

func newRemove(opts *options) *cobra.Command {
	var typ, planPath string
	var approve bool
	cmd := &cobra.Command{Use: "remove [<resource-name>]", Short: "Planeja ou move um recurso canônico para a lixeira", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if opts.scope != "global" && opts.scope != "project" {
			return fmt.Errorf("remove exige --scope global ou project")
		}
		root, err := resolveLibraryRoot(opts)
		if err != nil {
			return err
		}
		if planPath == "" {
			if len(args) != 1 || typ == "" {
				return fmt.Errorf("prévia: remove <nome> --type <tipo>; depois use --plan <arquivo> --yes")
			}
			if approve {
				return fmt.Errorf("gere e revise o plano primeiro; --yes só pode ser usado com --plan")
			}
			plan, err := library.PlanRemove(root, args[0], typ)
			if err != nil {
				return err
			}
			return output(cmd, &options{json: true}, plan)
		}
		if len(args) != 0 {
			return fmt.Errorf("não combine nome posicional com --plan")
		}
		if !approve {
			return fmt.Errorf("remove exige aprovação explícita com --yes")
		}
		b, err := readPlanFile(planPath)
		if err != nil {
			return err
		}
		var plan library.RemovePlan
		if err := json.Unmarshal(b, &plan); err != nil {
			return fmt.Errorf("plano JSON inválido: %w", err)
		}
		if plan.Root != root {
			return fmt.Errorf("plano pertence a outra biblioteca/escopo")
		}
		paths, err := platform.New().Resolve(opts.project)
		if err != nil {
			return err
		}
		entry, err := library.ApplyRemove(plan, root, paths.State)
		if err != nil {
			return err
		}
		return output(cmd, &options{json: true}, entry)
	}}
	cmd.Flags().StringVar(&typ, "type", "", "Tipo do recurso")
	cmd.Flags().StringVar(&planPath, "plan", "", "Aplica um plano JSON revisado")
	cmd.Flags().BoolVar(&approve, "yes", false, "Aprova a remoção para lixeira")
	return cmd
}

func newRestore(opts *options) *cobra.Command {
	var typ, planPath string
	var approve bool
	cmd := &cobra.Command{Use: "restore [<resource-name>]", Short: "Planeja ou restaura um recurso tombstoned", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if opts.scope != "global" && opts.scope != "project" {
			return fmt.Errorf("restore exige --scope global ou project")
		}
		root, err := resolveLibraryRoot(opts)
		if err != nil {
			return err
		}
		if planPath == "" {
			if len(args) != 1 || typ == "" {
				return fmt.Errorf("prévia: restore <nome> --type <tipo>; depois use --plan <arquivo> --yes")
			}
			if approve {
				return fmt.Errorf("gere e revise o plano primeiro; --yes só pode ser usado com --plan")
			}
			paths, err := platform.New().Resolve(opts.project)
			if err != nil {
				return err
			}
			plan, err := library.PlanRestore(root, paths.State, args[0], typ)
			if err != nil {
				return err
			}
			return output(cmd, &options{json: true}, plan)
		}
		if len(args) != 0 {
			return fmt.Errorf("não combine nome posicional com --plan")
		}
		if !approve {
			return fmt.Errorf("restore exige aprovação explícita com --yes")
		}
		b, err := readPlanFile(planPath)
		if err != nil {
			return err
		}
		var plan library.RestorePlan
		if err := json.Unmarshal(b, &plan); err != nil {
			return fmt.Errorf("plano JSON inválido: %w", err)
		}
		if plan.Root != root {
			return fmt.Errorf("plano pertence a outra biblioteca/escopo")
		}
		paths, err := platform.New().Resolve(opts.project)
		if err != nil {
			return err
		}
		entry, err := library.ApplyRestore(plan, root, paths.State)
		if err != nil {
			return err
		}
		return output(cmd, &options{json: true}, entry)
	}}
	cmd.Flags().StringVar(&typ, "type", "", "Tipo do recurso")
	cmd.Flags().StringVar(&planPath, "plan", "", "Aplica um plano JSON revisado")
	cmd.Flags().BoolVar(&approve, "yes", false, "Aprova a restauração")
	return cmd
}

func newHistory(opts *options) *cobra.Command {
	var typ string
	cmd := &cobra.Command{Use: "history <resource-name>", Short: "Mostra revisões locais de um recurso", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if opts.scope != "global" && opts.scope != "project" {
			return fmt.Errorf("history exige --scope global ou project")
		}
		root, err := resolveLibraryRoot(opts)
		if err != nil {
			return err
		}
		history, err := library.GetHistory(root, args[0], typ)
		if err != nil {
			return err
		}
		if opts.json {
			return output(cmd, opts, history)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "RESOURCE\tSTATUS\tCURRENT\tREVISION\tSHA256\tCREATED\tSNAPSHOT\n"); err != nil {
			return err
		}
		for _, revision := range history.Revisions {
			current := "no"
			if revision.Current {
				current = "yes"
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s/%s\t%s\t%s\t%s\t%s\t%s\t%s\n", history.Type, history.Name, history.Status, current, revision.ID, revision.SHA256, revision.CreatedAt.UTC().Format(time.RFC3339), revision.Snapshot); err != nil {
				return err
			}
		}
		return nil
	}}
	cmd.Flags().StringVar(&typ, "type", "", "Tipo do recurso")
	return cmd
}

func newDeployments(opts *options) *cobra.Command {
	cmd := &cobra.Command{Use: "deployments", Short: "Lista deployments AgentHub e oferece remoção planejada", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		paths, err := platform.New().Resolve(opts.project)
		if err != nil {
			return err
		}
		items, err := deployment.ListDeployments(paths.State, opts.scope)
		if err != nil {
			return err
		}
		if opts.json {
			return output(cmd, opts, items)
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "ID\tPROVIDER\tSCOPE\tRESOURCE\tSTATUS\tTARGET\tHASH"); err != nil {
			return err
		}
		for _, item := range items {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", item.ID, item.Provider, item.Scope, item.Slug, item.Status, item.TargetPath, item.ContentHash); err != nil {
				return err
			}
		}
		return nil
	}}
	cmd.AddCommand(newDeploymentRemove(opts))
	return cmd
}

func newOperations(opts *options) *cobra.Command {
	cmd := &cobra.Command{Use: "operations", Short: "Lista operações e planeja a recuperação de operações parciais", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		paths, err := platform.New().Resolve(opts.project)
		if err != nil {
			return err
		}
		operations, err := deployment.ListOperations(paths.State)
		if err != nil {
			return err
		}
		if opts.json {
			return output(cmd, opts, operations)
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "ID\tSTATUS\tPROVIDER\tSCOPE\tTARGET\tATTEMPTS"); err != nil {
			return err
		}
		for _, operation := range operations {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\t%d\n", operation.ID, operation.Status, operation.Provider, operation.Scope, operation.TargetPath, operation.Attempts); err != nil {
				return err
			}
		}
		return nil
	}}
	cmd.AddCommand(newOperationRecover(opts))
	return cmd
}

func newOperationRecover(opts *options) *cobra.Command {
	var planPath string
	var approve bool
	cmd := &cobra.Command{Use: "recover <operation-id>", Short: "Planeja ou aplica recuperação aprovada de uma operação", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		paths, err := platform.New().Resolve(opts.project)
		if err != nil {
			return err
		}
		operation, err := deployment.ReadOperation(paths.State, args[0])
		if err != nil {
			return err
		}
		if operation.Scope == "project" && paths.Project == "" {
			project, err := os.Getwd()
			if err != nil {
				return err
			}
			paths, err = platform.New().Resolve(project)
			if err != nil {
				return err
			}
		}
		targetRoot, err := providers.SkillRoot(operation.Provider, paths.Home, paths.Project, operation.Scope)
		if err != nil {
			return err
		}
		if planPath == "" {
			if approve {
				return fmt.Errorf("gere e revise o recovery plan primeiro; --yes só pode ser usado com --plan")
			}
			plan, err := deployment.PlanOperationRecovery(paths.State, args[0], targetRoot)
			if err != nil {
				return err
			}
			return output(cmd, &options{json: true}, plan)
		}
		if !approve {
			return fmt.Errorf("operations recover exige --yes")
		}
		b, err := readPlanFile(planPath)
		if err != nil {
			return err
		}
		var plan deployment.OperationRecoveryPlan
		if err := json.Unmarshal(b, &plan); err != nil {
			return fmt.Errorf("recovery plan JSON inválido: %w", err)
		}
		if plan.OperationID != args[0] {
			return fmt.Errorf("recovery plan pertence a outra operação")
		}
		result, err := deployment.ApplyOperationRecovery(plan, targetRoot, paths.State)
		if err != nil {
			return err
		}
		return output(cmd, &options{json: true}, result)
	}}
	cmd.Flags().StringVar(&planPath, "plan", "", "Recovery plan JSON revisado")
	cmd.Flags().BoolVar(&approve, "yes", false, "Aprova a recuperação planejada")
	return cmd
}

func newDeploymentRemove(opts *options) *cobra.Command {
	var provider, planPath string
	var approve bool
	cmd := &cobra.Command{Use: "remove <deployment-id>", Short: "Planeja ou remove um deployment AgentHub", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if opts.scope != "global" && opts.scope != "project" {
			return fmt.Errorf("deployments remove exige --scope global ou project")
		}
		if provider == "" {
			return fmt.Errorf("informe --provider")
		}
		project := opts.project
		if opts.scope == "project" && project == "" {
			var err error
			project, err = os.Getwd()
			if err != nil {
				return err
			}
		}
		paths, err := platform.New().Resolve(project)
		if err != nil {
			return err
		}
		targetRoot, err := providers.SkillRoot(provider, paths.Home, paths.Project, opts.scope)
		if err != nil {
			return err
		}
		if planPath == "" {
			if len(args) != 1 {
				return fmt.Errorf("prévia: deployments remove <deployment-id>; depois use --plan <arquivo> --yes")
			}
			if approve {
				return fmt.Errorf("gere e revise o plano primeiro; --yes só pode ser usado com --plan")
			}
			plan, err := deployment.PlanRemoveDeployment(args[0], provider, opts.scope, targetRoot, paths.State)
			if err != nil {
				return err
			}
			return output(cmd, &options{json: true}, plan)
		}
		if len(args) != 0 {
			return fmt.Errorf("não combine deployment-id posicional com --plan")
		}
		if !approve {
			return fmt.Errorf("deployments remove exige --yes")
		}
		b, err := readPlanFile(planPath)
		if err != nil {
			return err
		}
		var plan deployment.DeploymentRemovalPlan
		if err := json.Unmarshal(b, &plan); err != nil {
			return fmt.Errorf("plano JSON inválido: %w", err)
		}
		if plan.Provider != provider || plan.Scope != opts.scope {
			return fmt.Errorf("plano pertence a outro provider/escopo")
		}
		result, err := deployment.ApplyRemoveDeployment(plan, targetRoot, paths.State)
		if err != nil {
			return err
		}
		return output(cmd, &options{json: true}, result)
	}}
	cmd.Flags().StringVar(&provider, "provider", "", "Provider do deployment")
	cmd.Flags().StringVar(&planPath, "plan", "", "Aplica um plano JSON revisado")
	cmd.Flags().BoolVar(&approve, "yes", false, "Aprova a remoção do destino gerenciado")
	return cmd
}

func readPlanFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("plano deve ser arquivo regular de até 1 MiB")
	}
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 1<<20 {
		return nil, fmt.Errorf("plano excede 1 MiB")
	}
	return b, nil
}

func newPlan(opts *options) *cobra.Command {
	var provider string
	cmd := &cobra.Command{Use: "plan <skill-slug>", Short: "Gera um plano read-only para publicar uma skill", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if opts.scope != "global" && opts.scope != "project" {
			return fmt.Errorf("plan exige --scope global ou project")
		}
		libraryRoot, err := resolveLibraryRoot(opts)
		if err != nil {
			return err
		}
		project := opts.project
		if opts.scope == "project" && project == "" {
			project, err = os.Getwd()
			if err != nil {
				return err
			}
		}
		paths, err := platform.New().Resolve(project)
		if err != nil {
			return err
		}
		targetRoot, err := providers.SkillRoot(provider, paths.Home, paths.Project, opts.scope)
		if err != nil {
			return err
		}
		plan, err := deployment.PlanSkill(libraryRoot, args[0], provider, opts.scope, targetRoot, paths.State)
		if err != nil {
			return err
		}
		return output(cmd, &options{json: true}, plan)
	}}
	cmd.Flags().StringVar(&provider, "provider", "", "Provider: codex, claude-code ou opencode")
	return cmd
}

func newApply(opts *options) *cobra.Command {
	var planPath string
	var approve bool
	cmd := &cobra.Command{Use: "apply", Short: "Aplica um plano de skill aprovado", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !approve {
			return fmt.Errorf("apply exige aprovação explícita com --yes")
		}
		if planPath == "" {
			return fmt.Errorf("informe --plan <arquivo.json>")
		}
		planFile, err := os.Open(planPath)
		if err != nil {
			return err
		}
		defer planFile.Close()
		info, err := planFile.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("arquivo de plano deve ser regular")
		}
		if info.Size() > 1<<20 {
			return fmt.Errorf("arquivo de plano excede 1 MiB")
		}
		b, err := io.ReadAll(io.LimitReader(planFile, (1<<20)+1))
		if err != nil {
			return err
		}
		if len(b) > 1<<20 {
			return fmt.Errorf("arquivo de plano excede 1 MiB")
		}
		var plan deployment.SkillPlan
		if err := json.Unmarshal(b, &plan); err != nil {
			return fmt.Errorf("plano JSON inválido: %w", err)
		}
		if plan.Scope != "global" && plan.Scope != "project" {
			return fmt.Errorf("escopo do plano inválido")
		}
		libraryRoot, err := resolveLibraryRoot(&options{scope: plan.Scope, project: opts.project})
		if err != nil {
			return err
		}
		project := opts.project
		if plan.Scope == "project" && project == "" {
			project, err = os.Getwd()
			if err != nil {
				return err
			}
		}
		paths, err := platform.New().Resolve(project)
		if err != nil {
			return err
		}
		targetRoot, err := providers.SkillRoot(plan.Provider, paths.Home, paths.Project, plan.Scope)
		if err != nil {
			return err
		}
		result, err := deployment.Apply(plan, libraryRoot, targetRoot, paths.State)
		if err != nil {
			return err
		}
		return output(cmd, &options{json: true}, result)
	}}
	cmd.Flags().StringVar(&planPath, "plan", "", "Arquivo JSON gerado por plan")
	cmd.Flags().BoolVar(&approve, "yes", false, "Aprova a escrita no destino do provider")
	return cmd
}

func newRollback(opts *options) *cobra.Command {
	var provider string
	var approve bool
	cmd := &cobra.Command{Use: "rollback <deployment-id>", Short: "Remove uma skill aplicada sem alterações externas", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !approve {
			return fmt.Errorf("rollback exige aprovação explícita com --yes")
		}
		if opts.scope != "global" && opts.scope != "project" {
			return fmt.Errorf("rollback exige --scope global ou project")
		}
		project := opts.project
		if opts.scope == "project" && project == "" {
			var err error
			project, err = os.Getwd()
			if err != nil {
				return err
			}
		}
		paths, err := platform.New().Resolve(project)
		if err != nil {
			return err
		}
		targetRoot, err := providers.SkillRoot(provider, paths.Home, paths.Project, opts.scope)
		if err != nil {
			return err
		}
		if err := deployment.Rollback(args[0], provider, targetRoot, paths.State); err != nil {
			return err
		}
		return output(cmd, &options{json: true}, map[string]string{"deployment_id": args[0], "status": "rolled_back"})
	}}
	cmd.Flags().StringVar(&provider, "provider", "", "Provider do deployment")
	cmd.Flags().BoolVar(&approve, "yes", false, "Aprova a remoção do destino gerenciado")
	return cmd
}

func newAdd(opts *options) *cobra.Command {
	var typ, ref, subpath, name, planPath string
	var approve, dryRun bool
	cmd := &cobra.Command{Use: "add [<local-path|github:owner/repo|https-url>]", Short: "Planeja ou adiciona um recurso local/Git à biblioteca", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		libraryRoot, err := resolveLibraryRoot(opts)
		if err != nil {
			return err
		}
		if planPath == "" {
			if len(args) != 1 {
				return fmt.Errorf("informe uma origem local/Git para gerar prévia")
			}
			if approve {
				return fmt.Errorf("gere e revise o plano primeiro; --yes só pode ser usado com --plan")
			}
			gitSource := strings.HasPrefix(args[0], "github:") || strings.HasPrefix(args[0], "https://") || ref != "" || subpath != ""
			if typ == "" && !gitSource {
				typ = inferType(args[0])
			}
			if typ == "" && !gitSource {
				return fmt.Errorf("informe --type para tipo local não inferido")
			}
			var plan library.AddPlan
			if gitSource {
				plan, err = library.PlanAddGit(cmd.Context(), libraryRoot, args[0], ref, subpath, typ, name)
			} else {
				plan, err = library.PlanAddLocal(libraryRoot, args[0], typ, name)
			}
			if err != nil {
				return err
			}
			return output(cmd, &options{json: true}, plan)
		}
		if len(args) != 0 {
			return fmt.Errorf("não combine origem posicional com --plan")
		}
		if !approve {
			return fmt.Errorf("add exige aprovação explícita com --yes")
		}
		if dryRun {
			return fmt.Errorf("--dry-run não pode ser combinado com --plan/--yes")
		}
		b, err := readPlanFile(planPath)
		if err != nil {
			return err
		}
		var plan library.AddPlan
		if err := json.Unmarshal(b, &plan); err != nil {
			return fmt.Errorf("plano JSON inválido: %w", err)
		}
		if plan.Root != libraryRoot {
			return fmt.Errorf("plano pertence a outra biblioteca/escopo")
		}
		entry, err := library.ApplyAdd(cmd.Context(), plan, libraryRoot)
		if err != nil {
			return err
		}
		return output(cmd, &options{json: true}, entry)
	}}
	cmd.Flags().StringVar(&typ, "type", "", "Tipo do recurso")
	cmd.Flags().StringVar(&name, "name", "", "Nome/slug canônico")
	cmd.Flags().StringVar(&ref, "ref", "", "Branch, tag ou commit Git; padrão HEAD")
	cmd.Flags().StringVar(&subpath, "subpath", "", "Arquivo/diretório dentro do repositório Git")
	cmd.Flags().StringVar(&planPath, "plan", "", "Aplica um plano JSON revisado")
	cmd.Flags().BoolVar(&approve, "yes", false, "Aprova o add descrito pelo plano")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Gera prévia read-only (comportamento padrão sem --plan)")
	return cmd
}

func inferType(source string) string {
	p := strings.ToLower(filepath.ToSlash(source))
	for _, pair := range [][2]string{{"agents", "agents"}, {"skills", "skills"}, {"commands", "commands"}, {"rules", "rules"}} {
		if strings.Contains(p, "/"+pair[0]+"/") || strings.HasSuffix(p, "/"+pair[0]) {
			return pair[1]
		}
	}
	return ""
}

type importPlan struct {
	Version    int                   `json:"schema_version"`
	Provider   string                `json:"provider_id"`
	Root       string                `json:"library_root"`
	Candidates []importPlanCandidate `json:"candidates"`
	PlanHash   string                `json:"plan_hash"`
}

type importPlanCandidate struct {
	ID        string                 `json:"id"`
	Candidate models.ImportCandidate `json:"candidate"`
	AddPlan   *library.AddPlan       `json:"add_plan,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

type importResult struct {
	Status  string          `json:"status"`
	Applied []library.Entry `json:"applied"`
}

type scopedLibraryEntry struct {
	Scope string `json:"scope"`
	library.Entry
}

func newImport(opts *options) *cobra.Command {
	var from, planPath string
	var approve bool
	var selected []string
	cmd := &cobra.Command{Use: "import", Short: "Planeja e importa candidatos selecionados de um provider", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		provider := from
		if provider == "claude" {
			provider = "claude-code"
		}
		if provider != "codex" && provider != "claude-code" && provider != "opencode" {
			return fmt.Errorf("--from deve ser codex, claude ou opencode")
		}
		libraryRoot, err := resolveLibraryRoot(opts)
		if err != nil {
			return err
		}
		libraryRoot, err = filepath.Abs(libraryRoot)
		if err != nil {
			return err
		}
		if planPath == "" {
			if approve || len(selected) > 0 {
				return fmt.Errorf("gere e revise a prévia primeiro; --yes e --select exigem --plan")
			}
			inv, err := inventory(opts)
			if err != nil {
				return err
			}
			plan := importPlan{Version: 1, Provider: provider, Root: libraryRoot, Candidates: []importPlanCandidate{}}
			for _, item := range inv.Items {
				if item.Provider != provider || item.Type == models.MCP {
					continue
				}
				typ := string(item.Type) + "s"
				source := item.Path
				if item.Type == models.Skill {
					source = filepath.Dir(item.Path)
				}
				candidate := models.ImportCandidate{SourcePath: source, Provider: provider, Kind: item.Type, Slug: filepath.Base(source)}
				addPlan, planErr := library.PlanAddLocal(libraryRoot, source, typ, "")
				candidatePlan := importPlanCandidate{ID: strconv.Itoa(len(plan.Candidates) + 1), Candidate: candidate}
				if planErr != nil {
					candidatePlan.Error = planErr.Error()
				} else {
					candidatePlan.Candidate.Slug = addPlan.Name
					candidatePlan.Candidate.ContentHash = addPlan.ContentHash
					candidatePlan.AddPlan = &addPlan
				}
				plan.Candidates = append(plan.Candidates, candidatePlan)
			}
			sort.Slice(plan.Candidates, func(i, j int) bool {
				return plan.Candidates[i].Candidate.SourcePath < plan.Candidates[j].Candidate.SourcePath
			})
			targets := map[string][]int{}
			for i := range plan.Candidates {
				plan.Candidates[i].ID = strconv.Itoa(i + 1)
				if candidatePlan := plan.Candidates[i].AddPlan; candidatePlan != nil {
					key := strings.ToLower(filepath.Clean(candidatePlan.Target))
					targets[key] = append(targets[key], i)
				}
			}
			for _, indexes := range targets {
				if len(indexes) < 2 {
					continue
				}
				for _, index := range indexes {
					plan.Candidates[index].Error = "colisão: mais de uma origem aponta para o mesmo destino canônico"
					plan.Candidates[index].AddPlan = nil
				}
			}
			plan.PlanHash, err = importPlanDigest(plan)
			if err != nil {
				return err
			}
			return output(cmd, &options{json: true}, plan)
		}
		if !approve {
			return fmt.Errorf("import --plan exige aprovação explícita com --yes")
		}
		if len(selected) == 0 {
			return fmt.Errorf("selecione candidatos com --select (IDs da prévia)")
		}
		b, err := readPlanFile(planPath)
		if err != nil {
			return err
		}
		var plan importPlan
		if err := json.Unmarshal(b, &plan); err != nil {
			return fmt.Errorf("plano import inválido: %w", err)
		}
		digest, err := importPlanDigest(plan)
		if err != nil || plan.Version != 1 || digest != plan.PlanHash {
			return fmt.Errorf("integridade do plano import inválida")
		}
		if plan.Provider != provider || plan.Root != libraryRoot {
			return fmt.Errorf("plano pertence a outro provider ou biblioteca/escopo")
		}
		wanted := make(map[string]bool, len(selected))
		for _, id := range selected {
			if wanted[id] {
				return fmt.Errorf("candidato duplicado em --select: %s", id)
			}
			wanted[id] = true
		}
		chosen := make([]importPlanCandidate, 0, len(wanted))
		for _, candidate := range plan.Candidates {
			if !wanted[candidate.ID] {
				continue
			}
			if candidate.AddPlan == nil || candidate.Error != "" || candidate.AddPlan.Root != plan.Root || candidate.AddPlan.SourceKind != "local" || candidate.AddPlan.SourcePath != candidate.Candidate.SourcePath || candidate.AddPlan.Name != candidate.Candidate.Slug || candidate.AddPlan.Type != string(candidate.Candidate.Kind)+"s" || candidate.Candidate.Provider != plan.Provider {
				return fmt.Errorf("candidato %s não é importável pelo plano", candidate.ID)
			}
			current, err := library.PlanAddLocal(libraryRoot, candidate.AddPlan.SourcePath, candidate.AddPlan.Type, candidate.AddPlan.Name)
			if err != nil || current.PlanHash != candidate.AddPlan.PlanHash {
				return fmt.Errorf("candidato %s mudou após a prévia; gere novo plano", candidate.ID)
			}
			chosen = append(chosen, candidate)
		}
		if len(chosen) != len(wanted) {
			return fmt.Errorf("--select contém ID inexistente neste plano")
		}
		result := importResult{Status: "applied", Applied: []library.Entry{}}
		for _, candidate := range chosen {
			entry, err := library.ApplyAdd(cmd.Context(), *candidate.AddPlan, libraryRoot)
			if err != nil {
				result.Status = "partial"
				if outputErr := output(cmd, &options{json: true}, result); outputErr != nil {
					return outputErr
				}
				return fmt.Errorf("importados %d candidatos antes da falha no candidato %s: %w", len(result.Applied), candidate.ID, err)
			}
			result.Applied = append(result.Applied, entry)
		}
		return output(cmd, &options{json: true}, result)
	}}
	cmd.Flags().StringVar(&from, "from", "", "Provider de origem")
	cmd.Flags().StringVar(&planPath, "plan", "", "Aplica uma prévia JSON revisada")
	cmd.Flags().StringSliceVar(&selected, "select", nil, "IDs de candidatos da prévia; pode repetir")
	cmd.Flags().BoolVar(&approve, "yes", false, "Aprova somente os candidatos selecionados no plano")
	return cmd
}

func importPlanDigest(plan importPlan) (string, error) {
	plan.PlanHash = ""
	b, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func newMigration(opts *options) *cobra.Command {
	var sourceRoot, targetRoot, legacyLock string
	var dryRun bool
	cmd := &cobra.Command{Use: "migrate", Short: "Inspeciona dados legados sem alterá-los", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !dryRun {
			return fmt.Errorf("migrate atualmente oferece somente --dry-run")
		}
		if opts.scope == "project" || opts.project != "" {
			return fmt.Errorf("migrate --dry-run cobre apenas os roots globais antigos")
		}
		paths, err := platform.New().Resolve("")
		if err != nil {
			return err
		}
		if sourceRoot == "" {
			sourceRoot = filepath.Join(paths.State, "library")
		}
		if targetRoot == "" {
			targetRoot = paths.Library
		}
		if legacyLock == "" {
			legacyLock = filepath.Join(targetRoot, "agenthub.lock.json")
		}
		plan, err := library.PlanMigration(sourceRoot, targetRoot, legacyLock)
		if err != nil {
			return err
		}
		return output(cmd, &options{json: true}, plan)
	}}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Somente inventaria roots antigos e destinos; não escreve")
	cmd.Flags().StringVar(&sourceRoot, "source", "", "Root legado (padrão: ~/.agenthub/library)")
	cmd.Flags().StringVar(&targetRoot, "destination", "", "Biblioteca canônica de destino (padrão: ~/.agents)")
	cmd.Flags().StringVar(&legacyLock, "legacy-lock", "", "Lock legado para inventariar sem converter")
	return cmd
}

func resolveLibraryRoot(opts *options) (string, error) {
	project := opts.project
	if opts.scope == "project" && project == "" {
		var err error
		project, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	paths, err := platform.New().Resolve(project)
	if err != nil {
		return "", err
	}
	if opts.scope == "project" {
		if paths.ProjectLibrary == "" {
			return "", fmt.Errorf("projeto não resolvido")
		}
		return paths.ProjectLibrary, nil
	}
	if opts.scope != "all" && opts.scope != "global" {
		return "", fmt.Errorf("escopo inválido %q (use all, global ou project)", opts.scope)
	}
	return paths.Library, nil
}

func Execute() {
	if err := NewRootCommand(os.Stdout, os.Stderr).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "agenthub:", err)
		os.Exit(1)
	}
}

func inventory(opts *options) (models.Inventory, error) {
	if opts.scope != "all" && opts.scope != "global" && opts.scope != "project" {
		return models.Inventory{}, fmt.Errorf("escopo inválido %q (use all, global ou project)", opts.scope)
	}
	project := opts.project
	if project == "" {
		var err error
		project, err = os.Getwd()
		if err != nil {
			return models.Inventory{}, err
		}
	}
	project, err := filepath.Abs(project)
	if err != nil {
		return models.Inventory{}, err
	}
	global, project, err := platform.New().Roots(project)
	if err != nil {
		return models.Inventory{}, err
	}
	roots := make([]models.Root, 0)
	for _, adapter := range providers.All() {
		roots = append(roots, adapter.Roots(global, project)...)
	}
	filtered := roots[:0]
	for _, root := range roots {
		if opts.scope == "all" || opts.scope == root.Scope {
			filtered = append(filtered, root)
		}
	}
	items, err := discovery.Scan(filtered, providers.All())
	if err != nil {
		return models.Inventory{}, err
	}
	return registry.Build(items), nil
}

func output(cmd *cobra.Command, opts *options, value any) error {
	if opts.json {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	}
	return printText(cmd.OutOrStdout(), value)
}

func printText(w io.Writer, value any) error {
	var items []models.Item
	switch result := value.(type) {
	case []scopedLibraryEntry:
		if _, err := fmt.Fprintln(w, "SCOPE\tTYPE\tNAME\tSHA256"); err != nil {
			return err
		}
		for _, entry := range result {
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", entry.Scope, entry.Type, entry.Name, entry.SHA256); err != nil {
				return err
			}
		}
		return nil
	case models.Inventory:
		items = result.Items
	case []models.Item:
		items = result
	default:
		_, err := fmt.Fprintln(w, value)
		return err
	}
	if _, err := fmt.Fprintln(w, "PROVIDER\tTYPE\tNAME\tORIGIN\tPATH\tDUPLICATES"); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\n", item.Provider, item.Type, item.Name, item.Origin, item.Path, len(item.Duplicates)); err != nil {
			return err
		}
	}
	return nil
}

func newInit(opts *options) *cobra.Command {
	return &cobra.Command{Use: "init", Short: "Inicializa a configuração local do AgentHub", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		paths, err := platform.New().Resolve("")
		if err != nil {
			return err
		}
		target := filepath.Join(paths.State, "config.json")
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		data := []byte("{\n  \"version\": 1\n}\n")
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			if os.IsExist(err) {
				return output(cmd, opts, map[string]string{"config": target, "status": "existing"})
			}
			return err
		}
		if _, err = f.Write(data); err != nil {
			f.Close()
			os.Remove(target)
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		return output(cmd, opts, map[string]string{"config": target, "status": "created"})
	}}
}

func newScan(opts *options) *cobra.Command {
	return &cobra.Command{Use: "scan", Short: "Varre instalações e arquivos conhecidos", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		v, e := inventory(opts)
		if e != nil {
			return e
		}
		return output(cmd, opts, v)
	}}
}
func newList(opts *options) *cobra.Command {
	return &cobra.Command{Use: "list", Short: "Lista recursos do catálogo canônico", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		entries, err := listLibrary(opts)
		if err != nil {
			return err
		}
		return output(cmd, opts, entries)
	}}
}

func newShow(opts *options) *cobra.Command {
	return &cobra.Command{Use: "show <resource-id>", Short: "Mostra proveniência e revisão de um recurso canônico", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		entries, err := listLibrary(opts)
		if err != nil {
			return err
		}
		var match *scopedLibraryEntry
		for i := range entries {
			if entries[i].ID != args[0] {
				continue
			}
			if match != nil {
				return fmt.Errorf("resource id %q existe em mais de um escopo; use --scope global ou project", args[0])
			}
			match = &entries[i]
		}
		if match == nil {
			return fmt.Errorf("recurso canônico não encontrado: %s", args[0])
		}
		return output(cmd, opts, *match)
	}}
}

func newFind(opts *options) *cobra.Command {
	return &cobra.Command{Use: "find <query>", Short: "Busca metadata e texto em recursos knowledge", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		query := strings.ToLower(strings.TrimSpace(args[0]))
		if query == "" {
			return fmt.Errorf("consulta vazia")
		}
		entries, err := listLibrary(opts)
		if err != nil {
			return err
		}
		found := make([]scopedLibraryEntry, 0)
		seen := make(map[string]bool, len(entries))
		for _, item := range entries {
			fields := []string{item.Scope, item.ID, item.Name, item.Type, item.Origin, item.Status}
			if item.Source != nil {
				fields = append(fields, item.Source.Kind, item.Source.URL, item.Source.Ref, item.Source.Commit, item.Source.Subpath)
			}
			if strings.Contains(strings.ToLower(strings.Join(fields, "\x00")), query) {
				found = append(found, item)
				seen[item.Scope+"\x00"+item.ID] = true
			}
		}
		project := opts.project
		if opts.scope != "global" && project == "" {
			project, err = os.Getwd()
			if err != nil {
				return err
			}
		}
		paths, err := platform.New().Resolve(project)
		if err != nil {
			return err
		}
		roots := map[string]string{"global": paths.Library, "project": paths.ProjectLibrary}
		for _, item := range entries {
			key := item.Scope + "\x00" + item.ID
			if item.Type != "knowledge" || seen[key] {
				continue
			}
			matched, err := library.SearchKnowledgeResource(filepath.Join(roots[item.Scope], "knowledge", item.Name), query)
			if err != nil {
				return fmt.Errorf("buscando knowledge %s/%s: %w", item.Scope, item.Name, err)
			}
			if matched {
				found = append(found, item)
				seen[key] = true
			}
		}
		return output(cmd, opts, found)
	}}
}

func listLibrary(opts *options) ([]scopedLibraryEntry, error) {
	if opts.scope != "all" && opts.scope != "global" && opts.scope != "project" {
		return nil, fmt.Errorf("escopo inválido %q (use all, global ou project)", opts.scope)
	}
	project := opts.project
	if opts.scope != "global" && project == "" {
		var err error
		project, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	paths, err := platform.New().Resolve(project)
	if err != nil {
		return nil, err
	}
	roots := []struct{ scope, path string }{}
	if opts.scope == "all" || opts.scope == "global" {
		roots = append(roots, struct{ scope, path string }{"global", paths.Library})
	}
	if opts.scope == "all" || opts.scope == "project" {
		roots = append(roots, struct{ scope, path string }{"project", paths.ProjectLibrary})
	}
	entries := []scopedLibraryEntry{}
	for _, root := range roots {
		resources, err := library.List(root.path)
		if err != nil {
			return nil, err
		}
		for _, resource := range resources {
			entries = append(entries, scopedLibraryEntry{Scope: root.scope, Entry: resource})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Scope != entries[j].Scope {
			return entries[i].Scope < entries[j].Scope
		}
		if entries[i].Type != entries[j].Type {
			return entries[i].Type < entries[j].Type
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}
func newProviders(opts *options) *cobra.Command {
	return &cobra.Command{Use: "providers", Short: "Lista adapters e níveis de evidência", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return output(cmd, opts, providers.Describe()) }}
}
func newStatus(opts *options) *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Resume o inventário local", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		v, e := inventory(opts)
		if e != nil {
			return e
		}
		counts := map[string]int{}
		for _, item := range v.Items {
			counts[string(item.Type)]++
		}
		return output(cmd, opts, map[string]any{"items": len(v.Items), "by_type": counts, "duplicates": v.DuplicateGroups})
	}}
}
func newDoctor(opts *options) *cobra.Command {
	return &cobra.Command{Use: "doctor", Short: "Verifica acesso aos diretórios de descoberta", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		v, e := inventory(opts)
		if e != nil {
			return e
		}
		return output(cmd, opts, map[string]any{"status": "inventory_scanned", "providers": providers.Describe(), "items": len(v.Items)})
	}}
}
