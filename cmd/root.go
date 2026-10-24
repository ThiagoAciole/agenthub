package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ThiagoAciole/agenthub/internal/discovery"
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
	root.AddCommand(newInit(opts), newScan(opts), newList(opts), newProviders(opts), newStatus(opts), newDoctor(opts))
	return root
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
		dir, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		target := filepath.Join(dir, "agenthub", "config.json")
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		data := []byte("{\n  \"version\": 1\n}\n")
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			if os.IsExist(err) {
				return fmt.Errorf("configuração já existe: %s", target)
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
	return &cobra.Command{Use: "list", Short: "Lista itens encontrados", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		v, e := inventory(opts)
		if e != nil {
			return e
		}
		return output(cmd, opts, v.Items)
	}}
}
func newProviders(opts *options) *cobra.Command {
	return &cobra.Command{Use: "providers", Short: "Lista providers suportados", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return output(cmd, opts, providers.Names()) }}
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
		return output(cmd, opts, map[string]any{"status": "ok", "providers": providers.Names(), "items": len(v.Items)})
	}}
}
