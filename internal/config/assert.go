package config

type AssertConfig struct {
	Job       map[string]JobAssert      `yaml:"job"`
	Artifacts map[string]ArtifactAssert `yaml:"artifacts"`
	Git       *GitAssert                `yaml:"git"`
	API       map[string]APICallAssert  `yaml:"api"`
	Binary    map[string]BinaryAssert   `yaml:"binary"`
}

type JobAssert struct {
	ExitStatus any    `yaml:"exit-status"`
	Present    *bool  `yaml:"present"`
	When       string `yaml:"when"`
	Stdout     any    `yaml:"stdout"`
	Stderr     any    `yaml:"stderr"`
	Output     any    `yaml:"output"`
}

type ArtifactAssert struct {
	Exists   *bool         `yaml:"exists"`
	Contents any           `yaml:"contents"`
	Mode     string        `yaml:"mode"`
	Size     any           `yaml:"size"`
	MD5      string        `yaml:"md5"`
	SHA256   string        `yaml:"sha256"`
	Filetype string        `yaml:"filetype"`
	Report   *ReportAssert `yaml:"report"`
}

// ReportAssert parses a structured report file (junit, dotenv, coverage, etc.)
// and exposes typed fields for assertion. The Format field selects the parser.
type ReportAssert struct {
	Format string `yaml:"format"`

	// junit / coverage
	Tests    any              `yaml:"tests"`
	Failures any              `yaml:"failures"`
	Errors   any              `yaml:"errors"`
	Skipped  any              `yaml:"skipped"`
	Suites   []SuiteAssert    `yaml:"suites"`

	// coverage (Cobertura)
	LineRate   any `yaml:"line-rate"`
	BranchRate any `yaml:"branch-rate"`

	// dotenv
	Keys map[string]any `yaml:"keys"`

	// gitlab-security (sast, dast, dependency_scanning, container_scanning, secret_detection)
	Critical any `yaml:"critical"`
	High     any `yaml:"high"`
	Medium   any `yaml:"medium"`
	Low      any `yaml:"low"`
}

// SuiteAssert holds per-suite assertions for the junit format.
type SuiteAssert struct {
	Name     string `yaml:"name"`
	Tests    any    `yaml:"tests"`
	Failures any    `yaml:"failures"`
	Errors   any    `yaml:"errors"`
	Skipped  any    `yaml:"skipped"`
}

type GitAssert struct {
	Origin    *GitRepoAssert `yaml:"origin"`
	Workspace *GitRepoAssert `yaml:"workspace"`
}

type GitRepoAssert struct {
	Commits    any                       `yaml:"commits"`
	LastCommit *GitLastCommitAssert      `yaml:"last-commit"`
	File       map[string]ArtifactAssert `yaml:"file"`
	Branch     any                       `yaml:"branch"`
	Clean      *bool                     `yaml:"clean"`
}

type GitLastCommitAssert struct {
	AuthorName  any `yaml:"author-name"`
	AuthorEmail any `yaml:"author-email"`
	Message     any `yaml:"message"`
	SHA         any `yaml:"sha"`
}

type APICallAssert struct {
	Called *bool          `yaml:"called"`
	Times  any            `yaml:"times"`
	Body   map[string]any `yaml:"body"`
}

type BinaryAssert struct {
	Called          *bool              `yaml:"called"`
	Times           any                `yaml:"times"`
	Calls           []BinaryCallAssert `yaml:"calls"`
	NeverCalledWith *BinaryCallAssert  `yaml:"never-called-with"`
}

type BinaryCallAssert struct {
	Args  any `yaml:"args"`
	CWD   any `yaml:"cwd"`
	Stdin any `yaml:"stdin"`
}
