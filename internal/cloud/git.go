package cloud

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/ThiagoAciole/agenthub/internal/fsutil"
)

// InitBareRepository creates a local bare Git repository without replacing an existing path.
func InitBareRepository(ctx context.Context, destination string) error {
	destination, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if err := rejectRepositoryPath(destination); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("repositório já existe: %s", destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	workspace, err := os.MkdirTemp(parent, ".agenthub-bare-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workspace)
	bare := filepath.Join(workspace, "repo.git")
	if err := runGit(ctx, workspace, nil, "init", "--bare", "--quiet", bare); err != nil {
		return err
	}
	return fsutil.RenameNoReplace(bare, destination)
}

// CloneBareRepository clones only a local bare repository into a new isolated path.
func CloneBareRepository(ctx context.Context, remote, destination string) error {
	remote, err := filepath.Abs(remote)
	if err != nil {
		return err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	if pathsOverlap(remote, destination) {
		return fmt.Errorf("origem e destino do clone não podem se sobrepor")
	}
	if err := requireBareRepository(ctx, remote); err != nil {
		return err
	}
	if err := validateBareGitConfig(ctx, remote); err != nil {
		return err
	}
	if err := rejectRemoteHooks(remote); err != nil {
		return err
	}
	if err := rejectRepositoryPath(destination); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("destino do clone já existe: %s", destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	workspace, err := os.MkdirTemp(parent, ".agenthub-clone-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workspace)
	clone := filepath.Join(workspace, "repo.git")
	if err := runGit(ctx, workspace, nil, "clone", "--quiet", "--bare", "--no-hardlinks", "--", remote, clone); err != nil {
		return err
	}
	return fsutil.RenameNoReplace(clone, destination)
}

// FetchSnapshot extracts and validates remote main into a new isolated directory.
// The returned content is a review candidate; this function never writes to the canonical library.
func FetchSnapshot(ctx context.Context, remote, destination string) (Snapshot, string, error) {
	remote, err := filepath.Abs(remote)
	if err != nil {
		return Snapshot{}, "", err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return Snapshot{}, "", err
	}
	if pathsOverlap(remote, destination) {
		return Snapshot{}, "", fmt.Errorf("origem e destino do snapshot não podem se sobrepor")
	}
	if err := requireBareRepository(ctx, remote); err != nil {
		return Snapshot{}, "", err
	}
	if err := validateBareGitConfig(ctx, remote); err != nil {
		return Snapshot{}, "", err
	}
	if err := rejectRemoteHooks(remote); err != nil {
		return Snapshot{}, "", err
	}
	if err := rejectRepositoryPath(destination); err != nil {
		return Snapshot{}, "", err
	}
	if _, err := os.Lstat(destination); err == nil {
		return Snapshot{}, "", fmt.Errorf("destino do snapshot já existe: %s", destination)
	} else if !os.IsNotExist(err) {
		return Snapshot{}, "", err
	}
	commit, err := gitOutput(ctx, remote, "rev-parse", "--verify", "refs/heads/main^{commit}")
	if err != nil {
		return Snapshot{}, "", err
	}
	commitID := strings.TrimSpace(string(commit))
	if len(commitID) != 40 && len(commitID) != 64 || !isHexString(commitID) {
		return Snapshot{}, "", fmt.Errorf("commit remoto inválido")
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return Snapshot{}, "", err
	}
	workspace, err := os.MkdirTemp(parent, ".agenthub-fetch-*")
	if err != nil {
		return Snapshot{}, "", err
	}
	defer os.RemoveAll(workspace)
	stage := filepath.Join(workspace, "snapshot")
	if err := os.Mkdir(stage, 0700); err != nil {
		return Snapshot{}, "", err
	}
	if err := extractGitArchive(ctx, remote, commitID, stage); err != nil {
		return Snapshot{}, "", err
	}
	snapshot, err := readSnapshotManifest(stage)
	if err != nil {
		return Snapshot{}, "", err
	}
	if err := validateSnapshot(stage, snapshot); err != nil {
		return Snapshot{}, "", err
	}
	if err := fsutil.RenameNoReplace(stage, destination); err != nil {
		return Snapshot{}, "", err
	}
	return snapshot, commitID, nil
}

// CommitSnapshot commits only a recently approved, verified snapshot to its isolated staging repository.
func CommitSnapshot(ctx context.Context, staging string, expected Snapshot, message string) (string, error) {
	staging, err := filepath.Abs(staging)
	if err != nil {
		return "", err
	}
	if message == "" || strings.ContainsAny(message, "\r\n\x00") {
		return "", fmt.Errorf("mensagem de commit inválida")
	}
	if err := rejectRepositoryPath(staging); err != nil {
		return "", err
	}
	if err := validateSnapshot(staging, expected); err != nil {
		return "", err
	}
	gitDir := filepath.Join(staging, ".git")
	if _, err := os.Lstat(gitDir); os.IsNotExist(err) {
		if err := rejectParentRepositories(filepath.Dir(staging)); err != nil {
			return "", err
		}
		if err := runGit(ctx, staging, nil, "init", "--quiet"); err != nil {
			return "", err
		}
		if err := runGit(ctx, staging, nil, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	if err := requireWorktree(ctx, staging); err != nil {
		return "", err
	}
	if err := validateLocalGitConfig(ctx, staging); err != nil {
		return "", err
	}
	index, err := gitOutput(ctx, staging, "diff", "--cached", "--name-only", "--no-renames")
	if err != nil {
		return "", err
	}
	if len(index) != 0 {
		return "", fmt.Errorf("índice Git contém alterações preexistentes não revisadas")
	}
	args := []string{"add", "--all", "--force", "--", "agenthub.manifest.json"}
	for _, resource := range expected.Resources {
		if !resource.Tombstone {
			args = append(args, filepath.ToSlash(filepath.Join(resource.Type, resource.Name)))
		}
	}
	if err := runGit(ctx, staging, nil, args...); err != nil {
		return "", err
	}
	if err := runGit(ctx, staging, nil, "diff", "--cached", "--check"); err != nil {
		return "", err
	}
	status, err := gitOutput(ctx, staging, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	if len(status) == 0 {
		commit, err := gitOutput(ctx, staging, "rev-parse", "--verify", "HEAD^{commit}")
		return strings.TrimSpace(string(commit)), err
	}
	if err := runGit(ctx, staging, nil, "-c", "user.name=AgentHub", "-c", "user.email=agenthub@localhost", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", message); err != nil {
		return "", err
	}
	commit, err := gitOutput(ctx, staging, "rev-parse", "--verify", "HEAD^{commit}")
	return strings.TrimSpace(string(commit)), err
}

// PushSnapshot performs a normal fast-forward-only Git push to a local bare repository.
func PushSnapshot(ctx context.Context, staging string, expected Snapshot, remote string) error {
	staging, err := filepath.Abs(staging)
	if err != nil {
		return err
	}
	remote, err = filepath.Abs(remote)
	if err != nil {
		return err
	}
	if err := validateSnapshot(staging, expected); err != nil {
		return err
	}
	if err := requireWorktree(ctx, staging); err != nil {
		return err
	}
	if err := validateLocalGitConfig(ctx, staging); err != nil {
		return err
	}
	if err := requireBareRepository(ctx, remote); err != nil {
		return err
	}
	if err := validateBareGitConfig(ctx, remote); err != nil {
		return err
	}
	status, err := gitOutput(ctx, staging, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return err
	}
	if len(status) != 0 {
		return fmt.Errorf("staging tem alterações sem commit; push bloqueado")
	}
	url, err := gitOutput(ctx, staging, "remote", "get-url", "agenthub")
	if err != nil {
		if err := runGit(ctx, staging, nil, "remote", "add", "agenthub", remote); err != nil {
			return err
		}
	} else if filepath.Clean(strings.TrimSpace(string(url))) != filepath.Clean(remote) {
		return fmt.Errorf("remote agenthub já aponta para outro destino")
	}
	return runGit(ctx, staging, nil, "push", "--porcelain", "agenthub", "HEAD:refs/heads/main")
}

func validateSnapshot(staging string, expected Snapshot) error {
	if expected.Version != 1 || len(expected.Resources) == 0 {
		return fmt.Errorf("snapshot inválido ou vazio")
	}
	if err := fsutil.RejectLinkPath(staging); err != nil {
		return err
	}
	stageInfo, err := os.Lstat(staging)
	if err != nil {
		return err
	}
	if !stageInfo.IsDir() || fsutil.IsLinkOrJunction(stageInfo) {
		return fmt.Errorf("diretório de staging inválido")
	}
	b, err := os.ReadFile(filepath.Join(staging, "agenthub.manifest.json"))
	if err != nil {
		return err
	}
	expectedBytes, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		return err
	}
	if !bytes.Equal(b, append(expectedBytes, '\n')) {
		return fmt.Errorf("manifesto mudou após a seleção aprovada")
	}
	allowed := map[string]bool{"agenthub.manifest.json": true}
	seen := make(map[string]bool, len(expected.Resources))
	var totalBytes int64
	totalFiles := 0
	for _, resource := range expected.Resources {
		if resource.ResourceID == "" || resource.RevisionID == "" || !validSyncType(resource.Type) || !validSyncName(resource.Name) {
			return fmt.Errorf("entrada inválida no manifesto do snapshot")
		}
		key := strings.ToLower(filepath.Join(resource.Type, resource.Name))
		if seen[key] {
			return fmt.Errorf("resource path duplicado no manifesto")
		}
		seen[key] = true
		if resource.Tombstone {
			continue
		}
		root := filepath.Join(staging, resource.Type, resource.Name)
		size, count, err := validateSnapshotResource(root, resource.Hash)
		if err != nil {
			return err
		}
		totalBytes += size
		totalFiles += count
		if totalBytes > maxSnapshotBytes || totalFiles > maxSnapshotFiles {
			return fmt.Errorf("snapshot excede o limite de arquivos ou bytes")
		}
		allowed[filepath.ToSlash(filepath.Join(resource.Type, resource.Name))] = true
	}
	return filepath.WalkDir(staging, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == staging {
			return nil
		}
		rel, err := filepath.Rel(staging, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			if !entry.IsDir() {
				return fmt.Errorf("estrutura Git inválida no staging")
			}
			return filepath.SkipDir
		}
		if rel == "agenthub.manifest.json" || allowed[rel] {
			return nil
		}
		for _, resource := range expected.Resources {
			base := filepath.ToSlash(filepath.Join(resource.Type, resource.Name))
			if !resource.Tombstone && (rel == base || strings.HasPrefix(rel, base+"/")) {
				return nil
			}
			if strings.HasPrefix(base, rel+"/") {
				return nil
			}
		}
		return fmt.Errorf("arquivo não aprovado no snapshot: %s", rel)
	})
}

func validateSnapshotResource(root, expectedHash string) (int64, int, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return 0, 0, err
	}
	if fsutil.IsLinkOrJunction(info) || (!info.IsDir() && !info.Mode().IsRegular()) {
		return 0, 0, fmt.Errorf("resource inválido no staging")
	}
	h := sha256.New()
	files := 0
	var total int64
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		current, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if fsutil.IsLinkOrJunction(current) || (!current.IsDir() && !current.Mode().IsRegular()) {
			return fmt.Errorf("link ou arquivo especial bloqueado no staging")
		}
		if current.IsDir() {
			if path != root {
				rel, _ := filepath.Rel(root, path)
				if !validSyncName(filepath.Base(rel)) {
					return fmt.Errorf("nome de diretório inválido no snapshot")
				}
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !validSyncName(filepath.Base(rel)) {
			return fmt.Errorf("nome de arquivo inválido no snapshot")
		}
		name := filepath.ToSlash(rel)
		if strings.Contains(name, ".git/") {
			return fmt.Errorf("metadata Git não pode entrar no recurso")
		}
		base := strings.ToLower(filepath.Base(name))
		ext := strings.ToLower(filepath.Ext(base))
		if base == ".gitattributes" {
			return fmt.Errorf(".gitattributes não é aceito no snapshot Git")
		}
		if strings.HasPrefix(base, ".env") || base == "id_rsa" || base == "id_ed25519" || base == "credentials" || base == "secrets" || ext == ".pem" || ext == ".key" || ext == ".p12" || ext == ".pfx" {
			return fmt.Errorf("arquivo sensível bloqueado no staging: %s", filepath.Base(name))
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		opened, err := file.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(current, opened) {
			file.Close()
			return fmt.Errorf("arquivo mudou durante a validação do snapshot")
		}
		io.WriteString(h, name+"\x00")
		copied, err := io.Copy(h, file)
		if err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		if err := scanForSecrets(path); err != nil {
			return err
		}
		files++
		total += copied
		if files > maxSnapshotFiles || total > maxSnapshotBytes {
			return fmt.Errorf("snapshot excede o limite de arquivos")
		}
		return nil
	})
	if err != nil {
		return total, files, err
	}
	if hex.EncodeToString(h.Sum(nil)) != expectedHash {
		return total, files, fmt.Errorf("conteúdo do staging difere do hash aprovado")
	}
	return total, files, nil
}

func rejectRepositoryPath(path string) error {
	if err := fsutil.RejectLinkPath(filepath.Dir(path)); err != nil {
		return err
	}
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		if strings.EqualFold(filepath.Base(current), ".agents") {
			return fmt.Errorf("Git é proibido dentro da biblioteca canônica")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}

func rejectParentRepositories(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return fmt.Errorf("staging está dentro de outro repositório Git: %s", current)
		} else if !os.IsNotExist(err) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}

func requireWorktree(ctx context.Context, path string) error {
	out, err := gitOutput(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	root, err := filepath.Abs(strings.TrimSpace(string(out)))
	if err != nil {
		return fmt.Errorf("Git worktree não é isolado no diretório de snapshot")
	}
	rootInfo, rootErr := os.Stat(root)
	pathInfo, pathErr := os.Stat(path)
	if rootErr != nil || pathErr != nil || !os.SameFile(rootInfo, pathInfo) {
		return fmt.Errorf("Git worktree não é isolado no diretório de snapshot")
	}
	return nil
}

func validateLocalGitConfig(ctx context.Context, path string) error {
	keys, err := gitOutput(ctx, path, "config", "--local", "--no-includes", "--null", "--name-only", "--list")
	if err != nil {
		return err
	}
	allowed := map[string]bool{
		"core.repositoryformatversion": true,
		"core.filemode":                true,
		"core.bare":                    true,
		"core.logallrefupdates":        true,
		"core.ignorecase":              true,
		"core.symlinks":                true,
		"remote.agenthub.url":          true,
		"remote.agenthub.fetch":        true,
	}
	for _, key := range strings.Split(string(keys), "\x00") {
		if key != "" && !allowed[strings.ToLower(key)] {
			return fmt.Errorf("config Git local não permitida: %s", key)
		}
	}
	return nil
}

func validateBareGitConfig(ctx context.Context, path string) error {
	keys, err := gitOutput(ctx, path, "config", "--local", "--no-includes", "--null", "--name-only", "--list")
	if err != nil {
		return err
	}
	allowed := map[string]bool{
		"core.repositoryformatversion": true,
		"core.filemode":                true,
		"core.bare":                    true,
		"core.logallrefupdates":        true,
		"core.ignorecase":              true,
		"core.symlinks":                true,
	}
	for _, key := range strings.Split(string(keys), "\x00") {
		if key != "" && !allowed[strings.ToLower(key)] {
			return fmt.Errorf("config Git remoto não permitida: %s", key)
		}
	}
	return nil
}

func rejectRemoteHooks(remote string) error {
	hooks := filepath.Join(remote, "hooks")
	if err := fsutil.RejectLinkPath(hooks); err != nil {
		return err
	}
	entries, err := os.ReadDir(hooks)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".sample") {
			return fmt.Errorf("hook ativo no repositório remoto bloqueado: %s", entry.Name())
		}
	}
	return nil
}

func requireBareRepository(ctx context.Context, path string) error {
	if err := fsutil.RejectLinkPath(path); err != nil {
		return err
	}
	out, err := gitOutput(ctx, path, "rev-parse", "--is-bare-repository")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != "true" {
		return fmt.Errorf("destino Git não é repositório bare")
	}
	return nil
}

func gitOutput(ctx context.Context, directory string, args ...string) ([]byte, error) {
	return runGitOutput(ctx, directory, nil, args...)
}

func readSnapshotManifest(root string) (Snapshot, error) {
	manifestPath := filepath.Join(root, "agenthub.manifest.json")
	info, err := os.Lstat(manifestPath)
	if err != nil {
		return Snapshot{}, err
	}
	if fsutil.IsLinkOrJunction(info) || !info.Mode().IsRegular() {
		return Snapshot{}, fmt.Errorf("manifesto remoto não é arquivo regular")
	}
	if info.Size() > maxSnapshotManifestBytes {
		return Snapshot{}, fmt.Errorf("manifesto remoto excede o limite de bytes")
	}
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(b, &snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("manifesto remoto inválido: %w", err)
	}
	canonical, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return Snapshot{}, err
	}
	if !bytes.Equal(b, append(canonical, '\n')) {
		return Snapshot{}, fmt.Errorf("manifesto remoto não está no formato canônico")
	}
	return snapshot, nil
}

func extractGitArchive(ctx context.Context, repository, commit, destination string) error {
	workspace, err := os.MkdirTemp("", "agenthub-git-archive-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workspace)
	globalConfig := filepath.Join(workspace, "gitconfig")
	if err := os.WriteFile(globalConfig, nil, 0600); err != nil {
		return err
	}
	env := []string{"HOME=" + workspace, "USERPROFILE=" + workspace, "GIT_CONFIG_GLOBAL=" + globalConfig, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_ALLOW_PROTOCOL=file"}
	for _, key := range []string{"PATH", "SYSTEMROOT", "WINDIR", "TMP", "TEMP"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	cmd := exec.CommandContext(ctx, "git", "-c", "core.hooksPath="+workspace, "archive", "--format=tar", commit)
	cmd.Dir, cmd.Env = repository, env
	stderrFile, err := os.CreateTemp(workspace, "git-stderr-*.log")
	if err != nil {
		return err
	}
	defer stderrFile.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = stderrFile
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := extractTar(stdout, destination); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	if _, err := io.Copy(io.Discard, stdout); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("leitura do snapshot Git falhou: %w", err)
	}
	return nil
}

func extractTar(source io.Reader, destination string) error {
	reader := tar.NewReader(source)
	seen := make(map[string]bool)
	var total int64
	entriesCount := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		entriesCount++
		if entriesCount > maxSnapshotFiles {
			return fmt.Errorf("snapshot remoto excede o limite de entradas")
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			if header.Size > 1<<20 || len(header.PAXRecords) != 1 {
				return fmt.Errorf("PAX global header inválido no snapshot remoto")
			}
			total += header.Size
			for key, value := range header.PAXRecords {
				if key != "comment" || len(value) != 40 && len(value) != 64 || !isHexString(value) {
					return fmt.Errorf("metadado PAX não permitido no snapshot remoto")
				}
			}
			continue
		}
		name := strings.TrimSuffix(header.Name, "/")
		clean := path.Clean(name)
		if name == "" || clean != name || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || strings.Contains(clean, `\`) {
			return fmt.Errorf("path inseguro no snapshot remoto")
		}
		for _, segment := range strings.Split(clean, "/") {
			if !validSyncName(segment) {
				return fmt.Errorf("nome de arquivo remoto inválido: %q", segment)
			}
		}
		key := strings.ToLower(clean)
		if seen[key] {
			return fmt.Errorf("path duplicado no snapshot remoto: %s", clean)
		}
		seen[key] = true
		target := filepath.Join(destination, filepath.FromSlash(clean))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maxSnapshotBytes-total {
				return fmt.Errorf("snapshot remoto excede o limite de bytes")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			written, copyErr := io.CopyN(file, reader, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			total += written
		default:
			return fmt.Errorf("tipo de entrada Git não permitido no snapshot remoto: %d (%s)", header.Typeflag, clean)
		}
	}
}

func runGit(ctx context.Context, directory string, extraEnv []string, args ...string) error {
	_, err := runGitOutput(ctx, directory, extraEnv, args...)
	return err
}

func runGitOutput(ctx context.Context, directory string, extraEnv []string, args ...string) ([]byte, error) {
	workspace, err := os.MkdirTemp("", "agenthub-git-config-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workspace)
	globalConfig := filepath.Join(workspace, "gitconfig")
	if err := os.WriteFile(globalConfig, nil, 0600); err != nil {
		return nil, err
	}
	env := []string{"HOME=" + workspace, "USERPROFILE=" + workspace, "GIT_CONFIG_GLOBAL=" + globalConfig, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_ALLOW_PROTOCOL=file"}
	for _, key := range []string{"PATH", "SYSTEMROOT", "WINDIR", "TMP", "TEMP"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	env = append(env, extraEnv...)
	args = append([]string{"-c", "core.hooksPath=" + workspace}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir, cmd.Env = directory, env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s falhou: %w", args[2], err)
	}
	return out, nil
}

func isHexString(value string) bool {
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}
