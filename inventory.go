package main

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

// inventoryCache owns repository facts for a single run. Returned package
// inventories and parsed snapshots alias cached maps and slices, so callers must
// treat them as read-only unless they clone before mutating.
type inventoryCache struct {
	cfg            config
	git            gitRunner
	validRevisions map[string]struct{}
	files          map[revisionFileKey]cachedFile
	fileLists      map[string][]string
	packages       map[string]packageInventory
}

type revisionFileKey struct {
	Revision string
	Path     string
}

type cachedFile struct {
	existenceKnown bool
	exists         bool
	parsed         bool
	snapshot       parsedFileSnapshot
}

func newInventoryCache(cfg config, git gitRunner) *inventoryCache {
	return &inventoryCache{
		cfg:            cfg,
		git:            git,
		validRevisions: map[string]struct{}{},
		files:          map[revisionFileKey]cachedFile{},
		fileLists:      map[string][]string{},
		packages:       map[string]packageInventory{},
	}
}

func (cache *inventoryCache) ensureRevisionExists(ctx context.Context, revision string) error {
	if _, ok := cache.validRevisions[revision]; ok {
		return nil
	}
	if err := ensureRevisionExists(ctx, cache.cfg, cache.git, revision); err != nil {
		return err
	}
	cache.validRevisions[revision] = struct{}{}
	return nil
}

func (cache *inventoryCache) noteFileExists(revision, filePath string) {
	key := revisionFileKey{Revision: revision, Path: cleanGitPath(filePath)}
	file := cache.files[key]
	file.existenceKnown = true
	file.exists = true
	cache.files[key] = file
}

// parseFileAtRevision returns a parsed snapshot for an existing file. The
// returned snapshot aliases cache state and must be treated as read-only.
func (cache *inventoryCache) parseFileAtRevision(ctx context.Context, revision, filePath string) (parsedFileSnapshot, bool, error) {
	key := revisionFileKey{Revision: revision, Path: cleanGitPath(filePath)}
	file := cache.files[key]
	if file.parsed {
		return file.snapshot, true, nil
	}
	if err := cache.ensureRevisionExists(ctx, revision); err != nil {
		return parsedFileSnapshot{}, false, err
	}
	if !file.existenceKnown {
		exists, err := fileExistsAtRevision(ctx, cache.cfg, cache.git, revision, key.Path)
		if err != nil {
			return parsedFileSnapshot{}, false, err
		}
		file.existenceKnown = true
		file.exists = exists
		cache.files[key] = file
	}
	if !file.exists {
		return parsedFileSnapshot{}, false, nil
	}

	result, err := cache.git(ctx, cache.cfg.RepoRoot, "show", revision+":"+key.Path)
	if err != nil {
		return parsedFileSnapshot{}, false, fmt.Errorf("read %s at %s: %w", key.Path, revision, err)
	}
	parsed, err := parseSnapshotForPath(key.Path, []byte(result.Stdout))
	if err != nil {
		return parsedFileSnapshot{}, true, fmt.Errorf("parse %s at %s: %w", key.Path, revision, err)
	}
	file.parsed = true
	file.snapshot = parsed
	cache.files[key] = file
	return parsed, true, nil
}

func (cache *inventoryCache) parseChangeFileAtRevision(ctx context.Context, revision, filePath string) (parsedFileSnapshot, bool, error) {
	if filePath == "" || !isRunnableTestFilePath(filePath) {
		return parsedFileSnapshot{}, false, nil
	}
	return cache.parseFileAtRevision(ctx, revision, filePath)
}

// loadPackageInventory returns an inventory whose maps alias cache state. Callers
// must treat the result as read-only or clone maps before mutating.
func (cache *inventoryCache) loadPackageInventory(ctx context.Context, revision string, key packageKey) (packageInventory, error) {
	cacheKey := revision + "\x00" + key.Dir + "\x00" + key.Name
	if inventory, ok := cache.packages[cacheKey]; ok {
		return inventory, nil
	}

	files, err := cache.listTestFilesInDir(ctx, revision, key.Dir)
	if err != nil {
		return packageInventory{}, err
	}
	inventory := packageInventory{
		Key:   key,
		Tests: map[string]struct{}{},
	}
	for _, filePath := range files {
		parsed, exists, err := cache.parseFileAtRevision(ctx, revision, filePath)
		if err != nil {
			return packageInventory{}, err
		}
		if !exists || parsed.Snapshot.packageName != key.Name {
			continue
		}
		for testName := range parsed.Snapshot.tests {
			inventory.Tests[testName] = struct{}{}
		}
	}
	cache.packages[cacheKey] = inventory
	return inventory, nil
}

func (cache *inventoryCache) listTestFilesInDir(ctx context.Context, revision, dir string) ([]string, error) {
	cleanDir := filepath.ToSlash(filepath.Clean(dir))
	cacheKey := revision + "\x00" + cleanDir
	if files, ok := cache.fileLists[cacheKey]; ok {
		return files, nil
	}
	if err := cache.ensureRevisionExists(ctx, revision); err != nil {
		return nil, err
	}
	pathspec := cmp.Or(cleanDir, ".")
	result, err := cache.git(ctx, cache.cfg.RepoRoot, "ls-tree", "-r", "-z", "--name-only", revision, "--", pathspec)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	for part := range strings.SplitSeq(result.Stdout, "\x00") {
		if part == "" {
			continue
		}
		filePath := cleanGitPath(part)
		if !isRunnableTestFilePath(filePath) {
			continue
		}
		if filepath.ToSlash(filepath.Dir(filePath)) != cleanDir {
			continue
		}
		files = append(files, filePath)
		cache.noteFileExists(revision, filePath)
	}
	slices.Sort(files)
	cache.fileLists[cacheKey] = files
	return files, nil
}

func (cache *inventoryCache) directoryWideSelections(ctx context.Context, revision, dir string, files map[string]struct{}) ([]*packageSelection, error) {
	inventories, err := cache.loadDirectoryInventories(ctx, revision, dir)
	if err != nil {
		return nil, err
	}
	selections := make([]*packageSelection, 0, len(inventories))
	for _, inventory := range inventories {
		selection := allPackageTestsSelectionForFiles(inventory, maps.Clone(files))
		if selection == nil {
			continue
		}
		selections = append(selections, selection)
	}
	return selections, nil
}

func (cache *inventoryCache) loadDirectoryInventories(ctx context.Context, revision, dir string) ([]packageInventory, error) {
	files, err := cache.listTestFilesInDir(ctx, revision, dir)
	if err != nil {
		return nil, err
	}
	packageNames := map[string]struct{}{}
	for _, filePath := range files {
		parsed, exists, err := cache.parseFileAtRevision(ctx, revision, filePath)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		packageNames[parsed.Snapshot.packageName] = struct{}{}
	}
	keys := make([]packageKey, 0, len(packageNames))
	for packageName := range packageNames {
		keys = append(keys, packageKey{Dir: filepath.ToSlash(filepath.Clean(dir)), Name: packageName})
	}
	slices.SortFunc(keys, func(left, right packageKey) int {
		return cmp.Compare(left.Name, right.Name)
	})
	inventories := make([]packageInventory, 0, len(keys))
	for _, key := range keys {
		inventory, err := cache.loadPackageInventory(ctx, revision, key)
		if err != nil {
			return nil, err
		}
		inventories = append(inventories, inventory)
	}
	return inventories, nil
}
