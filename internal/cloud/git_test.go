package cloud

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotCommitPushAndBareClone(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	entry := addSkill(t, root, "shared", "initial portable content\n")
	staging := filepath.Join(t.TempDir(), "agenthub", "worktree")
	snapshot, err := BuildSnapshot(root, staging, map[string]bool{entry.ID: true})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := CommitSnapshot(ctx, staging, snapshot, "Initial snapshot")
	if err != nil || commit == "" {
		t.Fatalf("snapshot commit failed: commit=%q err=%v", commit, err)
	}
	if repeated, err := CommitSnapshot(ctx, staging, snapshot, "Retry snapshot"); err != nil || repeated != commit {
		t.Fatalf("identical snapshot was not idempotent: commit=%q err=%v", repeated, err)
	}
	remote := filepath.Join(t.TempDir(), "private.git")
	if err := InitBareRepository(ctx, remote); err != nil {
		t.Fatal(err)
	}
	if err := PushSnapshot(ctx, staging, snapshot, remote); err != nil {
		t.Fatal(err)
	}
	remoteHead, err := gitOutput(ctx, remote, "rev-parse", "refs/heads/main")
	if err != nil || string(remoteHead) != commit+"\n" {
		t.Fatalf("bare remote did not receive snapshot commit: %q %v", remoteHead, err)
	}
	clone := filepath.Join(t.TempDir(), "mirror.git")
	if err := CloneBareRepository(ctx, remote, clone); err != nil {
		t.Fatal(err)
	}
	cloneHead, err := gitOutput(ctx, clone, "rev-parse", "refs/heads/main")
	if err != nil || string(cloneHead) != commit+"\n" {
		t.Fatalf("isolated bare clone differs from remote: %q %v", cloneHead, err)
	}
	fetched := filepath.Join(t.TempDir(), "received")
	fetchedSnapshot, fetchedCommit, err := FetchSnapshot(ctx, remote, fetched)
	if err != nil {
		t.Fatal(err)
	}
	if fetchedCommit != commit || len(fetchedSnapshot.Resources) != 1 || fetchedSnapshot.Resources[0].ResourceID != entry.ID {
		t.Fatalf("fetched snapshot metadata differs: commit=%s snapshot=%+v", fetchedCommit, fetchedSnapshot)
	}
	if got, err := os.ReadFile(filepath.Join(fetched, "skills", entry.Name, "SKILL.md")); err != nil || string(got) != "initial portable content\n" {
		t.Fatalf("fetched snapshot content differs: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(fetched, ".git")); !os.IsNotExist(err) {
		t.Fatalf("remote .git metadata entered pending snapshot: %v", err)
	}
}

func TestGitBackendRejectsCanonicalAndExistingTargets(t *testing.T) {
	ctx := context.Background()
	canonical := filepath.Join(t.TempDir(), ".agents")
	if err := os.Mkdir(canonical, 0700); err != nil {
		t.Fatal(err)
	}
	if err := InitBareRepository(ctx, filepath.Join(canonical, "private.git")); err == nil {
		t.Fatal("initialized Git inside the canonical shared library")
	}
	existing := filepath.Join(t.TempDir(), "existing.git")
	if err := os.Mkdir(existing, 0700); err != nil {
		t.Fatal(err)
	}
	if err := InitBareRepository(ctx, existing); err == nil {
		t.Fatal("replaced an existing destination")
	}
	if entries, err := os.ReadDir(existing); err != nil || len(entries) != 0 {
		t.Fatalf("existing destination changed: entries=%v err=%v", entries, err)
	}
}

func TestCommitSnapshotRejectsLocalGitFilters(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	entry := addSkill(t, root, "skill", "portable content\n")
	staging := filepath.Join(t.TempDir(), "worktree")
	snapshot, err := BuildSnapshot(root, staging, map[string]bool{entry.ID: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CommitSnapshot(ctx, staging, snapshot, "Initial"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, staging, nil, "config", "filter.hostile.clean", "write-host-file"); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitSnapshot(ctx, staging, snapshot, "Retry"); err == nil {
		t.Fatal("accepted a local Git clean filter")
	}
}

func TestPushSnapshotRejectsActiveRemoteHooks(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	entry := addSkill(t, root, "skill", "portable content\n")
	staging := filepath.Join(t.TempDir(), "worktree")
	snapshot, err := BuildSnapshot(root, staging, map[string]bool{entry.ID: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CommitSnapshot(ctx, staging, snapshot, "Initial"); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err := InitBareRepository(ctx, remote); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("exit 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PushSnapshot(ctx, staging, snapshot, remote); err == nil {
		t.Fatal("pushed to a repository with an active hook")
	}
	if _, err := os.Stat(filepath.Join(remote, "refs", "heads", "main")); !os.IsNotExist(err) {
		t.Fatalf("remote ref changed despite hook rejection: %v", err)
	}
}

func TestExtractTarRejectsTraversalAndLinks(t *testing.T) {
	for _, header := range []tar.Header{
		{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0600, Size: 1},
		{Name: "skills/link", Typeflag: tar.TypeSymlink, Linkname: "../../outside", Mode: 0777},
	} {
		var archive bytes.Buffer
		writer := tar.NewWriter(&archive)
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := writer.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := extractTar(bytes.NewReader(archive.Bytes()), t.TempDir()); err == nil {
			t.Fatalf("accepted unsafe archive entry: %+v", header)
		}
	}
}
