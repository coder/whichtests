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
	Key   packageKey
	Tests map[string]struct{}
	Files map[string]struct{}
}

type parsedFileSnapshot struct {
	Key      packageKey
	Snapshot fileSnapshot
}

// selectChange handles the four diff states for a runnable test file: add
// (old absent, new present), delete (old present, new absent), in-place modify
// (both sides present with the same package key), and cross-package move or
// package rename (both sides present with different package keys).
func selectChange(ctx context.Context, cache *inventoryCache, selections map[packageKey]*packageSelection, change testFileChange) error {
	cfg := cache.cfg
	hunks, err := listDiffHunks(ctx, cfg, cache.git, change)
	if err != nil {
		return fmt.Errorf("list diff hunks for %s: %w", change.displayPath(), err)
	}
	if len(hunks) == 0 {
		return nil
	}

	oldParsed, oldExists, err := cache.parseChangeFileAtRevision(ctx, cfg.BaseSHA, change.OldPath)
	if err != nil {
		return fmt.Errorf("resolve old package for %s: %w", change.displayPath(), err)
	}
	newParsed, newExists, err := cache.parseChangeFileAtRevision(ctx, cfg.HeadSHA, change.NewPath)
	if err != nil {
		return fmt.Errorf("resolve new package for %s: %w", change.displayPath(), err)
	}
	if change.expectsOldFile() && !oldExists {
		return fmt.Errorf("base revision %s is missing %s", cfg.BaseSHA, change.OldPath)
	}
	if change.expectsNewFile() && !newExists {
		return fmt.Errorf("head revision %s is missing %s", cfg.HeadSHA, change.NewPath)
	}

	var oldFile *parsedFileSnapshot
	if oldExists {
		oldFile = &oldParsed
	}
	var newFile *parsedFileSnapshot
	if newExists {
		newFile = &newParsed
	}

	if newFile != nil {
		inventory, err := cache.loadPackageInventory(ctx, cfg.HeadSHA, newFile.Key)
		if err != nil {
			return fmt.Errorf("load package inventory for %s: %w", newFile.Key.String(), err)
		}
		var oldSnapshot *fileSnapshot
		selectionHunks := hunks
		if oldFile != nil && oldFile.Key == newFile.Key {
			oldSnapshot = &oldFile.Snapshot
		} else {
			selectionHunks = newSideOnlyHunks(hunks)
		}
		selection := selectTestsFromHunks(change, oldSnapshot, newFile.Snapshot, inventory, selectionHunks)
		if err := mergeSelection(ctx, cache, selections, selection); err != nil {
			return err
		}
	}

	if oldFile != nil && (newFile == nil || oldFile.Key != newFile.Key) {
		inventory, err := cache.loadPackageInventory(ctx, cfg.HeadSHA, oldFile.Key)
		if err != nil {
			return fmt.Errorf("load package inventory for %s: %w", oldFile.Key.String(), err)
		}
		sourceChange := testFileChange{Kind: changeDeleted, OldPath: change.OldPath}
		selection := selectSourceRemoval(sourceChange, oldFile.Snapshot, inventory, hunks)
		if err := mergeSelection(ctx, cache, selections, selection); err != nil {
			return err
		}
	}

	return nil
}

func (change testFileChange) expectsOldFile() bool {
	oldRequired, _ := change.Kind.expectedFileSides()
	return oldRequired && isRunnableTestFilePath(change.OldPath)
}

func (change testFileChange) expectsNewFile() bool {
	_, newRequired := change.Kind.expectedFileSides()
	return newRequired && isRunnableTestFilePath(change.NewPath)
}

func (kind changeKind) expectedFileSides() (oldRequired bool, newRequired bool) {
	switch kind {
	case changeAdded:
		return false, true
	case changeDeleted:
		return true, false
	case changeModified, changeRenamed, changeType:
		return true, true
	}
	// parseChangeKind rejects unknown statuses before selection. This default is a
	// safety net for direct callers or future drift, but it only triggers
	// missing-file errors when the change carries runnable test paths.
	return true, true
}

func parseSnapshotForPath(filePath string, data []byte) (parsedFileSnapshot, error) {
	snapshot, err := parseFileSnapshot(data)
	if err != nil {
		return parsedFileSnapshot{}, fmt.Errorf("parse package clause: %w", err)
	}
	return parsedFileSnapshot{
		Key:      packageKey{Dir: filepath.ToSlash(filepath.Dir(filePath)), Name: snapshot.packageName},
		Snapshot: snapshot,
	}, nil
}

func mergeSelection(_ context.Context, _ *inventoryCache, selections map[packageKey]*packageSelection, selection *packageSelection) error {
	if selection == nil || len(selection.Tests) == 0 {
		return nil
	}
	mergePackageSelection(selections, selection)
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
	maps.Copy(merged.Files, selection.Files)
	maps.Copy(merged.Tests, selection.Tests)
}

func selectTestsFromHunks(change testFileChange, oldSnapshot *fileSnapshot, newSnapshot fileSnapshot, newInventory packageInventory, hunks []diffHunk) *packageSelection {
	selected := map[string]struct{}{}
	for _, hunk := range hunks {
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
