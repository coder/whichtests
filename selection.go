package main

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"

	"golang.org/x/xerrors"
)

type packageKey struct {
	Dir  string
	Name string
}

type testDecl struct {
	FilePath string
	Range    lineRange
}

type packageInventory struct {
	Key   packageKey
	Tests map[string][]testDecl
}

func (inventory packageInventory) allTests() []string {
	return slices.Sorted(maps.Keys(inventory.Tests))
}

func (inventory packageInventory) hasTest(name string) bool {
	_, ok := inventory.Tests[name]
	return ok
}

type packageSelection struct {
	Key           packageKey
	Tests         map[string]struct{}
	Files         map[string]struct{}
	Broadened     bool
	DirectoryWide bool
}

func selectChange(ctx context.Context, cfg config, git gitRunner, cache *inventoryCache, selections map[packageKey]*packageSelection, change testFileChange) error {
	hunks, err := listDiffHunks(ctx, cfg, git, change)
	if err != nil {
		return xerrors.Errorf("list diff hunks for %s: %w", change.displayPath(), err)
	}
	if len(hunks) == 0 {
		return nil
	}

	oldData, oldExists, err := readChangeFile(ctx, cfg, git, cfg.BaseSHA, change.OldPath)
	if err != nil {
		return err
	}
	newData, newExists, err := readChangeFile(ctx, cfg, git, cfg.HeadSHA, change.NewPath)
	if err != nil {
		return err
	}
	if change.NewPath != "" && isRunnableTestFilePath(change.NewPath) && !newExists {
		return xerrors.Errorf("head revision %s is missing %s", cfg.HeadSHA, change.NewPath)
	}

	var oldKey packageKey
	oldKeyOK := oldExists
	if oldExists {
		oldKey, err = packageKeyForData(change.OldPath, oldData)
		if err != nil {
			return xerrors.Errorf("resolve old package for %s: %w", change.displayPath(), err)
		}
	}
	var newKey packageKey
	newKeyOK := newExists
	if newExists {
		newKey, err = packageKeyForData(change.NewPath, newData)
		if err != nil {
			return xerrors.Errorf("resolve new package for %s: %w", change.displayPath(), err)
		}
	}

	if newKeyOK {
		inventory, err := cache.loadPackageInventory(ctx, cfg.HeadSHA, newKey)
		if err != nil {
			return xerrors.Errorf("load package inventory for %s: %w", newKey.String(), err)
		}
		selectionOldData := oldData
		selectionHunks := hunks
		if !oldKeyOK || oldKey != newKey {
			selectionOldData = nil
			selectionHunks = newSideOnlyHunks(hunks)
		}
		selection := selectTestsForSnapshots(change, selectionOldData, newData, inventory, selectionHunks)
		if err := mergeSelection(ctx, cache, cfg.HeadSHA, selections, selection); err != nil {
			return err
		}
	}

	if oldKeyOK && (!newKeyOK || oldKey != newKey) {
		inventory, err := cache.loadPackageInventory(ctx, cfg.HeadSHA, oldKey)
		if err != nil {
			return xerrors.Errorf("load package inventory for %s: %w", oldKey.String(), err)
		}
		sourceChange := testFileChange{Kind: changeDeleted, OldPath: change.OldPath}
		selection := selectSourceRemovalForSnapshots(sourceChange, oldData, inventory, hunks)
		if err := mergeSelection(ctx, cache, cfg.HeadSHA, selections, selection); err != nil {
			return err
		}
	}

	return nil
}

func packageKeyForData(filePath string, data []byte) (packageKey, error) {
	snapshot, err := parseFileSnapshot(data)
	if err != nil {
		packageName, ok := fallbackPackageName(data)
		if !ok {
			return packageKey{}, xerrors.Errorf("parse package clause: %w", err)
		}
		return packageKey{Dir: filepath.ToSlash(filepath.Dir(filePath)), Name: packageName}, nil
	}
	return packageKey{Dir: filepath.ToSlash(filepath.Dir(filePath)), Name: snapshot.packageName}, nil
}

func mergeSelection(ctx context.Context, cache *inventoryCache, revision string, selections map[packageKey]*packageSelection, selection *packageSelection) error {
	if selection == nil {
		return nil
	}
	if !selection.DirectoryWide {
		if len(selection.Tests) > 0 {
			mergePackageSelection(selections, selection)
		}
		return nil
	}

	expanded, err := cache.directoryWideSelections(ctx, revision, selection.Key.Dir, selection.Files)
	if err != nil {
		return xerrors.Errorf("load directory-wide inventory for %s: %w", packagePattern(selection.Key.Dir), err)
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

func selectTestsForSnapshots(change testFileChange, oldData, newData []byte, newInventory packageInventory, hunks []diffHunk) *packageSelection {
	newSnapshot, err := parseFileSnapshot(newData)
	if err != nil {
		return allPackageTestsSelection(newInventory, change.displayPath())
	}

	if oldData == nil && needsOldSnapshot(hunks) {
		return allPackageTestsSelection(newInventory, change.displayPath())
	}

	var oldSnapshot *fileSnapshot
	if len(oldData) > 0 {
		snapshot, err := parseFileSnapshot(oldData)
		if err != nil {
			if needsOldSnapshot(hunks) {
				return allPackageTestsSelection(newInventory, change.displayPath())
			}
		} else {
			oldSnapshot = &snapshot
		}
	}

	selected := map[string]struct{}{}
	for _, hunk := range hunks {
		if oldSnapshot != nil {
			switch scope := broadeningScopeForOldHunk(oldSnapshot.shared, hunk.Old); scope {
			case broadeningDirectory:
				return allDirectoryTestsSelection(newInventory, change.displayPath())
			case broadeningPackage:
				return allPackageTestsSelection(newInventory, change.displayPath())
			}
		}
		switch scope := broadeningScopeForNewHunk(newSnapshot.shared, oldSnapshot, hunk.New); scope {
		case broadeningDirectory:
			return allDirectoryTestsSelection(newInventory, change.displayPath())
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
			if newInventory.hasTest(name) {
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

func selectSourceRemovalForSnapshots(change testFileChange, oldData []byte, inventory packageInventory, hunks []diffHunk) *packageSelection {
	oldSnapshot, err := parseFileSnapshot(oldData)
	if err != nil {
		if needsOldSnapshot(hunks) {
			return allPackageTestsSelection(inventory, change.displayPath())
		}
		return nil
	}

	selected := map[string]struct{}{}
	for _, hunk := range hunks {
		switch scope := broadeningScopeForOldHunk(oldSnapshot.shared, hunk.Old); scope {
		case broadeningDirectory:
			return allDirectoryTestsSelection(inventory, change.displayPath())
		case broadeningPackage:
			return allPackageTestsSelection(inventory, change.displayPath())
		}
		for name, declRange := range oldSnapshot.tests {
			if declRange.overlaps(hunk.Old) && inventory.hasTest(name) {
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
	for _, testName := range inventory.allTests() {
		selection.Tests[testName] = struct{}{}
	}
	if len(selection.Tests) == 0 {
		return nil
	}
	return selection
}

func allDirectoryTestsSelection(inventory packageInventory, filePath string) *packageSelection {
	selection := allPackageTestsSelection(inventory, filePath)
	if selection == nil {
		selection = &packageSelection{
			Key:       inventory.Key,
			Tests:     map[string]struct{}{},
			Files:     map[string]struct{}{filePath: {}},
			Broadened: true,
		}
	}
	selection.DirectoryWide = true
	return selection
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
