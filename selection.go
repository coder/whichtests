package main

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
)

type packageKey struct {
	Dir  string
	Name string
}

type packageInventory struct {
	Key   packageKey
	Tests map[string]struct{}
}

type packageSelection struct {
	Key           packageKey
	Tests         map[string]struct{}
	Files         map[string]struct{}
	Broadened     bool
	DirectoryWide bool
}

type parsedFileSnapshot struct {
	key      packageKey
	snapshot fileSnapshot
}

func selectChange(ctx context.Context, cache *inventoryCache, selections map[packageKey]*packageSelection, change testFileChange) error {
	cfg := cache.cfg
	hunks, err := listDiffHunks(ctx, cfg, cache.git, change)
	if err != nil {
		return fmt.Errorf("list diff hunks for %s: %w", change.displayPath(), err)
	}
	if len(hunks) == 0 {
		return nil
	}

	oldData, oldExists, err := readChangeFile(ctx, cfg, cache.git, cfg.BaseSHA, change.OldPath)
	if err != nil {
		return err
	}
	newData, newExists, err := readChangeFile(ctx, cfg, cache.git, cfg.HeadSHA, change.NewPath)
	if err != nil {
		return err
	}
	if isRunnableTestFilePath(change.NewPath) && !newExists {
		return fmt.Errorf("head revision %s is missing %s", cfg.HeadSHA, change.NewPath)
	}

	var oldFile *parsedFileSnapshot
	if oldExists {
		parsed, err := parseSnapshotForPath(change.OldPath, oldData)
		if err != nil {
			return fmt.Errorf("resolve old package for %s: %w", change.displayPath(), err)
		}
		oldFile = &parsed
	}
	var newFile *parsedFileSnapshot
	if newExists {
		parsed, err := parseSnapshotForPath(change.NewPath, newData)
		if err != nil {
			return fmt.Errorf("resolve new package for %s: %w", change.displayPath(), err)
		}
		newFile = &parsed
	}

	if newFile != nil {
		inventory, err := cache.loadPackageInventory(ctx, cfg.HeadSHA, newFile.key)
		if err != nil {
			return fmt.Errorf("load package inventory for %s: %w", newFile.key.String(), err)
		}
		var oldSnapshot *fileSnapshot
		selectionHunks := hunks
		if oldFile != nil && oldFile.key == newFile.key {
			oldSnapshot = &oldFile.snapshot
		} else {
			selectionHunks = newSideOnlyHunks(hunks)
		}
		selection := selectTestsFromHunks(change, oldSnapshot, newFile.snapshot, inventory, selectionHunks)
		if err := mergeSelection(ctx, cache, selections, selection); err != nil {
			return err
		}
	}

	if oldFile != nil && (newFile == nil || oldFile.key != newFile.key) {
		inventory, err := cache.loadPackageInventory(ctx, cfg.HeadSHA, oldFile.key)
		if err != nil {
			return fmt.Errorf("load package inventory for %s: %w", oldFile.key.String(), err)
		}
		sourceChange := testFileChange{Kind: changeDeleted, OldPath: change.OldPath}
		selection := selectSourceRemoval(sourceChange, oldFile.snapshot, inventory, hunks)
		if err := mergeSelection(ctx, cache, selections, selection); err != nil {
			return err
		}
	}

	return nil
}

func parseSnapshotForPath(filePath string, data []byte) (parsedFileSnapshot, error) {
	snapshot, err := parseFileSnapshot(data)
	if err != nil {
		return parsedFileSnapshot{}, fmt.Errorf("parse package clause: %w", err)
	}
	return parsedFileSnapshot{
		key:      packageKey{Dir: filepath.ToSlash(filepath.Dir(filePath)), Name: snapshot.packageName},
		snapshot: snapshot,
	}, nil
}

func mergeSelection(ctx context.Context, cache *inventoryCache, selections map[packageKey]*packageSelection, selection *packageSelection) error {
	if selection == nil {
		return nil
	}
	if !selection.DirectoryWide {
		if len(selection.Tests) > 0 {
			mergePackageSelection(selections, selection)
		}
		return nil
	}

	expanded, err := cache.directoryWideSelections(ctx, cache.cfg.HeadSHA, selection.Key.Dir, selection.Files)
	if err != nil {
		return fmt.Errorf("load directory-wide inventory for %s: %w", packagePattern(selection.Key.Dir), err)
	}
	for _, expandedSelection := range expanded {
		mergePackageSelection(selections, expandedSelection)
	}
	return nil
}

func mergePackageSelection(selections map[packageKey]*packageSelection, selection *packageSelection) {
	merged := selections[selection.Key]
	if merged == nil {
		merged = &packageSelection{
			Key:   selection.Key,
			Tests: map[string]struct{}{},
			Files: map[string]struct{}{},
		}
		selections[selection.Key] = merged
	}
	merged.Broadened = merged.Broadened || selection.Broadened
	maps.Copy(merged.Files, selection.Files)
	maps.Copy(merged.Tests, selection.Tests)
}

func selectTestsFromHunks(change testFileChange, oldSnapshot *fileSnapshot, newSnapshot fileSnapshot, newInventory packageInventory, hunks []diffHunk) *packageSelection {
	if oldSnapshot == nil && needsOldSnapshot(hunks) {
		return allPackageTestsSelection(newInventory, change.displayPath())
	}

	selected := map[string]struct{}{}
	for _, hunk := range hunks {
		if oldSnapshot != nil {
			switch scope := broadeningScopeForOldHunk(oldSnapshot.shared, hunk.Old); scope {
			case broadeningDirectory:
				return allDirectoryTestsSelection(newInventory.Key.Dir, change.displayPath())
			case broadeningPackage:
				return allPackageTestsSelection(newInventory, change.displayPath())
			}
		}
		switch scope := broadeningScopeForNewHunk(newSnapshot.shared, oldSnapshot, hunk.New); scope {
		case broadeningDirectory:
			return allDirectoryTestsSelection(newInventory.Key.Dir, change.displayPath())
		case broadeningPackage:
			return allPackageTestsSelection(newInventory, change.displayPath())
		}
		addMatchingTests(selected, newSnapshot.tests, hunk.New)
		if oldSnapshot == nil {
			continue
		}
		for name, declRange := range oldSnapshot.tests {
			if !declRange.overlaps(hunk.Old) {
				continue
			}
			if _, ok := newInventory.Tests[name]; ok {
				selected[name] = struct{}{}
			}
		}
	}
	if len(selected) == 0 {
		return nil
	}
	return &packageSelection{
		Key:   newInventory.Key,
		Tests: selected,
		Files: map[string]struct{}{change.displayPath(): {}},
	}
}

func selectSourceRemoval(change testFileChange, oldSnapshot fileSnapshot, inventory packageInventory, hunks []diffHunk) *packageSelection {
	selected := map[string]struct{}{}
	for _, hunk := range hunks {
		switch scope := broadeningScopeForOldHunk(oldSnapshot.shared, hunk.Old); scope {
		case broadeningDirectory:
			return allDirectoryTestsSelection(inventory.Key.Dir, change.displayPath())
		case broadeningPackage:
			return allPackageTestsSelection(inventory, change.displayPath())
		}
		for name, declRange := range oldSnapshot.tests {
			if !declRange.overlaps(hunk.Old) {
				continue
			}
			if _, ok := inventory.Tests[name]; ok {
				selected[name] = struct{}{}
			}
		}
	}
	if len(selected) == 0 {
		return nil
	}
	return &packageSelection{
		Key:   inventory.Key,
		Tests: selected,
		Files: map[string]struct{}{change.displayPath(): {}},
	}
}

func allPackageTestsSelection(inventory packageInventory, filePath string) *packageSelection {
	return allPackageTestsSelectionForFiles(inventory, map[string]struct{}{filePath: {}})
}

func allPackageTestsSelectionForFiles(inventory packageInventory, files map[string]struct{}) *packageSelection {
	selection := &packageSelection{
		Key:       inventory.Key,
		Tests:     map[string]struct{}{},
		Files:     files,
		Broadened: true,
	}
	maps.Copy(selection.Tests, inventory.Tests)
	if len(selection.Tests) == 0 {
		return nil
	}
	return selection
}

func allDirectoryTestsSelection(dir, filePath string) *packageSelection {
	return &packageSelection{
		Key:           packageKey{Dir: dir},
		Files:         map[string]struct{}{filePath: {}},
		DirectoryWide: true,
	}
}

func needsOldSnapshot(hunks []diffHunk) bool {
	for _, hunk := range hunks {
		if hunk.Old.hasLines() {
			return true
		}
	}
	return false
}

func addMatchingTests(selected map[string]struct{}, tests map[string]lineRange, candidate lineRange) {
	for name, declRange := range tests {
		if declRange.overlaps(candidate) {
			selected[name] = struct{}{}
		}
	}
}

func (key packageKey) String() string {
	return fmt.Sprintf("%s (%s)", packagePattern(key.Dir), key.Name)
}

func packagePattern(dir string) string {
	cleanDir := filepath.ToSlash(filepath.Clean(dir))
	if cleanDir == "." {
		return "."
	}
	return "./" + cleanDir
}
