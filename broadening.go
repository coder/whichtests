package main

type broadeningScope uint8

const (
	broadeningNone broadeningScope = iota
	broadeningPackage
	broadeningDirectory
)

func broadeningScopeForOldHunk(decls []sharedDecl, candidate lineRange) broadeningScope {
	scope := broadeningNone
	for _, decl := range decls {
		if !decl.Range.overlaps(candidate) {
			continue
		}
		scope = max(scope, decl.broadeningScope())
	}
	return scope
}

func broadeningScopeForNewHunk(decls []sharedDecl, oldSnapshot *fileSnapshot, candidate lineRange) broadeningScope {
	scope := broadeningNone
	for _, decl := range decls {
		if !decl.Range.overlaps(candidate) {
			continue
		}
		scope = max(scope, decl.broadeningScopeOnNewSide(oldSnapshot))
	}
	return scope
}

func (decl sharedDecl) broadeningScope() broadeningScope {
	switch decl.Kind {
	case sharedDeclInit, sharedDeclTestMain:
		// Go builds package and package_test files into one test binary.
		// Init and TestMain changes can affect every test in the directory.
		return broadeningDirectory
	case sharedDeclImport, sharedDeclVar, sharedDeclConst, sharedDeclType, sharedDeclHelper:
		return broadeningPackage
	}
	return broadeningNone
}

func (decl sharedDecl) broadeningScopeOnNewSide(oldSnapshot *fileSnapshot) broadeningScope {
	switch decl.Kind {
	// TODO: Decide whether new imports should narrow to tests that still
	// reference package-local declarations. Today any import edit broadens
	// the package.
	case sharedDeclImport:
		return broadeningPackage
	case sharedDeclInit, sharedDeclTestMain:
		return broadeningDirectory
	case sharedDeclVar, sharedDeclConst, sharedDeclType, sharedDeclHelper:
		if oldSnapshot != nil && oldSnapshot.hasSharedKey(decl.Keys) {
			return broadeningPackage
		}
	}
	return broadeningNone
}
