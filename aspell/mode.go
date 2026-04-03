package aspell

type mode string

const (
	modeDisabled mode = "disabled"
	modeSubject  mode = "subject"
	modeCommit   mode = "commit"
	modeAll      mode = "all"
)

type identifierScope string

const (
	identifierScopeDiff  identifierScope = "diff"
	identifierScopeFiles identifierScope = "files"
	identifierScopeAll   identifierScope = "all"
)
