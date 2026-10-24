package library

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const maxGitFiles = 10000
const maxGitBytes = 512 << 20

type gitResource struct {
	Workspace     string
	ContentPath   string
	FetchURL      string
	ProvenanceURL string
	Ref           string
	Commit        string
	Subpath       string
	Name          string
}

func PlanAddGit(ctx context.Context, root, repository, ref, subpath, typ, name string) (AddPlan, error) {
	resource, err := fetchGitResource(ctx, repository, ref, subpath)
	if err != nil {
		return AddPlan{}, err
	}
	defer os.RemoveAll(resource.Workspace)
	if typ == "" {
		typ = inferGitResourceType(resource)
	}
	return buildGitAddPlan(root, resource, typ, name)
}

func inferGitResourceType(resource gitResource) string {
	if strings.EqualFold(filepath.Base(resource.ContentPath), "SKILL.md") {
		return "skills"
	}
	if info, err := os.Stat(filepath.Join(resource.ContentPath, "SKILL.md")); err == nil && info.Mode().IsRegular() {
		return "skills"
	}
	for _, typ := range []string{"agents", "skills", "prompts", "commands", "rules", "memory", "mcps", "plugins", "knowledge", "profiles"} {
		if resource.Subpath == typ || strings.HasPrefix(resource.Subpath, typ+"/") {
			return typ
		}
	}
	return ""
}

func buildGitAddPlan(root string, resource gitResource, typ, name string) (AddPlan, error) {
	if !validType(typ) {
		return AddPlan{}, fmt.Errorf("tipo inválido: %s", typ)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return AddPlan{}, err
	}
	if name == "" {
		name = resource.Name
	}
	if !validResourceName(name) {
		return AddPlan{}, fmt.Errorf("nome de recurso Git inválido")
	}
	if err := checkAddAvailable(root, typ, name); err != nil {
		return AddPlan{}, err
	}
	hash, err := resourceContentHash(resource.ContentPath, typ)
	if err != nil {
		return AddPlan{}, err
	}
	files, err := resourceFiles(resource.ContentPath)
	if err != nil {
		return AddPlan{}, err
	}
	plan := AddPlan{Version: 1, Root: root, Type: typ, Name: name, Target: filepath.Join(root, typ, name), SourceKind: "git", GitURL: resource.FetchURL, GitRef: resource.Ref, GitCommit: resource.Commit, GitPath: resource.Subpath, ContentHash: hash, Files: files, Risk: "Copia somente blobs regulares da revisão fixada; não faz checkout, não inclui .git e não executa scripts ou hooks."}
	plan.PlanHash, err = addPlanDigest(plan)
	return plan, err
}

func fetchGitResource(ctx context.Context, repository, ref, subpath string) (gitResource, error) {
	fetchURL, provenanceURL, err := normalizeGitRepository(repository)
	if err != nil {
		return gitResource{}, err
	}
	if ref == "" {
		ref = "HEAD"
	}
	if err := validateGitRef(ctx, ref); err != nil {
		return gitResource{}, err
	}
	subpath, err = cleanGitSubpath(subpath)
	if err != nil {
		return gitResource{}, err
	}
	workspace, err := os.MkdirTemp("", "agenthub-git-")
	if err != nil {
		return gitResource{}, err
	}
	fail := func(err error) (gitResource, error) { _ = os.RemoveAll(workspace); return gitResource{}, err }
	template := filepath.Join(workspace, "empty-template")
	if err := os.Mkdir(template, 0700); err != nil {
		return fail(err)
	}
	config := filepath.Join(workspace, "empty.gitconfig")
	if err := os.WriteFile(config, nil, 0600); err != nil {
		return fail(err)
	}
	env := gitEnv(workspace, config)
	bare := filepath.Join(workspace, "repo.git")
	if _, err := runGit(ctx, env, workspace, "init", "--bare", "--quiet", "--template", template, bare); err != nil {
		return fail(err)
	}
	if _, err := runGit(ctx, env, bare, "fetch", "--no-tags", "--no-recurse-submodules", "--depth=1", fetchURL, ref); err != nil {
		return fail(fmt.Errorf("git fetch falhou: %w", err))
	}
	commitOut, err := runGit(ctx, env, bare, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return fail(fmt.Errorf("ref Git não resolve para commit: %w", err))
	}
	commit := strings.TrimSpace(string(commitOut))
	if len(commit) != 40 && len(commit) != 64 {
		return fail(fmt.Errorf("hash de commit Git inválido"))
	}
	treeExpr := commit + "^{tree}"
	if subpath != "" {
		treeExpr = commit + ":" + subpath
	}
	typeOut, err := runGit(ctx, env, bare, "cat-file", "-t", treeExpr)
	if err != nil {
		return fail(fmt.Errorf("caminho Git não encontrado na revisão: %w", err))
	}
	objectType := strings.TrimSpace(string(typeOut))
	if objectType != "tree" && objectType != "blob" {
		return fail(fmt.Errorf("caminho Git deve ser diretório ou arquivo regular"))
	}
	args := []string{"--literal-pathspecs", "ls-tree", "-r", "-z", "--full-tree", commit}
	if subpath != "" {
		args = append(args, "--", subpath)
	}
	tree, err := runGit(ctx, env, bare, args...)
	if err != nil {
		return fail(fmt.Errorf("não foi possível ler a árvore Git: %w", err))
	}
	contentRoot := filepath.Join(workspace, "resource")
	if err := os.Mkdir(contentRoot, 0700); err != nil {
		return fail(err)
	}
	if objectType == "blob" && subpath == "" {
		return fail(fmt.Errorf("repositório Git não pode ser um blob"))
	}
	seen := make(map[string]bool)
	files := make([]gitFile, 0)
	for _, record := range bytes.Split(tree, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		parts := bytes.SplitN(record, []byte{'\t'}, 2)
		if len(parts) != 2 {
			return fail(fmt.Errorf("entrada ls-tree inválida"))
		}
		metadata := strings.Fields(string(parts[0]))
		if len(metadata) != 3 || metadata[1] != "blob" || metadata[0] != "100644" && metadata[0] != "100755" {
			return fail(fmt.Errorf("Git contém symlink, submódulo ou objeto não regular"))
		}
		rel := string(parts[1])
		if objectType == "tree" && subpath != "" {
			prefix := subpath + "/"
			if !strings.HasPrefix(rel, prefix) {
				return fail(fmt.Errorf("entrada Git fora do subdiretório selecionado"))
			}
			rel = strings.TrimPrefix(rel, prefix)
		} else if objectType == "blob" {
			rel = path.Base(subpath)
		}
		if err := validatePortableRelativePath(rel); err != nil {
			return fail(err)
		}
		portable := strings.ToLower(rel)
		if seen[portable] {
			return fail(fmt.Errorf("colisão de nomes case-insensitive na árvore Git: %s", rel))
		}
		seen[portable] = true
		files = append(files, gitFile{OID: metadata[2], Path: rel})
	}
	if len(files) == 0 {
		return fail(fmt.Errorf("recurso Git selecionado não contém arquivos"))
	}
	if len(files) > maxGitFiles {
		return fail(fmt.Errorf("árvore Git excede o limite de arquivos"))
	}
	if err := materializeGitBlobs(ctx, env, bare, contentRoot, files); err != nil {
		return fail(err)
	}
	contentPath := contentRoot
	if objectType == "blob" {
		contentPath = filepath.Join(contentRoot, path.Base(subpath))
	}
	name := path.Base(subpath)
	if subpath == "" {
		if strings.HasPrefix(fetchURL, "https://") {
			u, _ := url.Parse(fetchURL)
			name = strings.TrimSuffix(path.Base(strings.TrimSuffix(u.Path, "/")), ".git")
		} else {
			name = strings.TrimSuffix(filepath.Base(strings.TrimSuffix(fetchURL, string(filepath.Separator))), ".git")
		}
	}
	if objectType == "blob" {
		name = strings.TrimSuffix(name, path.Ext(name))
	}
	return gitResource{Workspace: workspace, ContentPath: contentPath, FetchURL: fetchURL, ProvenanceURL: provenanceURL, Ref: ref, Commit: commit, Subpath: subpath, Name: name}, nil
}

type gitFile struct{ OID, Path string }

func materializeGitBlobs(ctx context.Context, env []string, bare, root string, files []gitFile) error {
	var input bytes.Buffer
	for _, file := range files {
		input.WriteString(file.OID)
		input.WriteByte('\n')
	}
	cmd := exec.CommandContext(ctx, "git", "cat-file", "--batch")
	cmd.Dir, cmd.Env, cmd.Stdin = bare, env, &input
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	reader := bufio.NewReader(stdout)
	var total int64
	for _, file := range files {
		header, err := reader.ReadString('\n')
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("resposta cat-file inválida: %w", err)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[1] != "blob" || fields[0] != file.OID {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("objeto Git não é blob regular")
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 || size > maxGitBytes-total {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("árvore Git excede o limite de bytes")
		}
		total += size
		target := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
		_, copyErr := io.CopyN(out, reader, size)
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		}
		terminator, err := reader.ReadByte()
		if err != nil || terminator != '\n' {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("terminador de blob Git inválido")
		}
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("leitura de blobs Git falhou: %w", err)
	}
	return nil
}

func normalizeGitRepository(input string) (fetchURL, provenanceURL string, err error) {
	if strings.HasPrefix(input, "github:") {
		parts := strings.Split(strings.TrimPrefix(input, "github:"), "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("use github:owner/repo")
		}
		for _, part := range parts {
			if !validGitName(part) {
				return "", "", fmt.Errorf("owner/repo Git inválido")
			}
		}
		parts[1] = strings.TrimSuffix(parts[1], ".git")
		fetchURL = "https://github.com/" + parts[0] + "/" + parts[1] + ".git"
		return fetchURL, fetchURL, nil
	}
	if strings.Contains(input, "://") {
		u, parseErr := url.Parse(input)
		if parseErr != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return "", "", fmt.Errorf("somente URL HTTPS sem credenciais, query ou fragmento é aceita")
		}
		for _, part := range strings.Split(u.Path, "/") {
			if part == ".." {
				return "", "", fmt.Errorf("path traversal na URL Git")
			}
		}
		return u.String(), u.String(), nil
	}
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("origem Git deve ser HTTPS, github:owner/repo ou repositório local")
	}
	return abs, "local-git", nil
}

func validateGitRef(ctx context.Context, ref string) error {
	if ref == "HEAD" {
		return nil
	}
	if strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, " \t\r\n\x00") {
		return fmt.Errorf("ref Git inválida")
	}
	if (len(ref) == 40 || len(ref) == 64) && isHex(ref) {
		return nil
	}
	if strings.HasPrefix(ref, "refs/") {
		cmd := exec.CommandContext(ctx, "git", "check-ref-format", ref)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("ref Git inválida")
		}
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "check-ref-format", "--branch", ref)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ref Git inválida")
	}
	return nil
}

func cleanGitSubpath(value string) (string, error) {
	if value == "" || value == "." {
		return "", nil
	}
	if strings.Contains(value, `\`) || strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("subpath Git inválido")
	}
	clean := path.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, "../") || clean != value {
		return "", fmt.Errorf("subpath Git deve ser relativo e normalizado")
	}
	return clean, nil
}

func validatePortableRelativePath(value string) error {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, `\`) {
		return fmt.Errorf("path Git perigoso")
	}
	for _, segment := range strings.Split(value, "/") {
		if strings.EqualFold(segment, ".git") {
			return fmt.Errorf("metadata .git não pode entrar no recurso")
		}
		if !validPortableResourceName(segment) {
			return fmt.Errorf("nome de arquivo Git inválido: %q", segment)
		}
	}
	return nil
}

func validGitName(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
			return false
		}
	}
	return true
}

func gitEnv(workspace, globalConfig string) []string {
	keys := []string{"PATH", "SYSTEMROOT", "WINDIR", "TMP", "TEMP"}
	env := make([]string, 0, len(keys)+8)
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	env = append(env, "HOME="+workspace, "USERPROFILE="+workspace, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+globalConfig, "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https:file", "GIT_OPTIONAL_LOCKS=0")
	return env
}

func runGit(ctx context.Context, env []string, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir, cmd.Env = dir, env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}

func isHex(value string) bool {
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}
