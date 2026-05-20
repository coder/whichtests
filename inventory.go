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

type inventoryCache struct {
	cfg       config
	git       gitRunner
	fileLists map[string][]string
	packages  map[string]packageInventory
}

func newInventoryCache(cfg config, git gitRunner) *inventoryCache {
	return &inventoryCache{
		cfg:       cfg,
		git:       git,
		fileLists: map[string][]string{},
		packages:  map[string]packageInventory{},
	}
}

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
		data, exists, err := readFileAtRevision(ctx, cache.cfg, cache.git, revision, filePath)
		if err != nil {
			return packageInventory{}, err
		}
		if !exists {
			continue
		}
		snapshot, err := parseFileSnapshot(data)
		if err != nil {
			return packageInventory{}, fmt.Errorf("parse %s at %s: %w", filePath, revision, err)
		}
		if snapshot.packageName != key.Name {
			continue
		}
		for testName := range snapshot.tests {
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
	pathspec := cleanDir
	if pathspec == "" {
		pathspec = "."
	}
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
		data, exists, err := readFileAtRevision(ctx, cache.cfg, cache.git, revision, filePath)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		snapshot, err := parseFileSnapshot(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s at %s: %w", filePath, revision, err)
		}
		packageNames[snapshot.packageName] = struct{}{}
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
