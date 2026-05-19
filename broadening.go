package main

type broadeningScope uint8

const (
	broadeningNone broadeningScope = iota
	broadeningPackage
	broadeningDirectory
)

func broadeningScopeForOldHunk(decls []sharedDecl, candidate lineRange) broadeningScope {
	for _, decl := range decls {
		if decl.Range.overlaps(candidate) {
			return decl.broadeningScope()
		}
	}
	return broadeningNone
}

func broadeningScopeForNewHunk(decls []sharedDecl, oldSnapshot *fileSnapshot, candidate lineRange) broadeningScope {
	for _, decl := range decls {
		if !decl.Range.overlaps(candidate) {
			continue
		}
		if scope := decl.broadeningScopeOnNewSide(oldSnapshot); scope != broadeningNone {
			return scope
		}
	}
	return broadeningNone
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
