package config

type AssertConfig struct {
	Job       map[string]JobAssert      `yaml:"job"`
	Artifacts map[string]ArtifactAssert `yaml:"artifacts"`
	Git       *GitAssert                `yaml:"git"`
	API       map[string]APICallAssert  `yaml:"api"`
	Binary    map[string]BinaryAssert   `yaml:"binary"`
}

type JobAssert struct {
	ExitStatus any   `yaml:"exit-status"`
	Present    *bool `yaml:"present"`
	Stdout     any   `yaml:"stdout"`
	Stderr     any   `yaml:"stderr"`
}

type ArtifactAssert struct {
	Exists   *bool  `yaml:"exists"`
	Contents any    `yaml:"contents"`
	Mode     string `yaml:"mode"`
	Size     any    `yaml:"size"`
	MD5      string `yaml:"md5"`
	SHA256   string `yaml:"sha256"`
	Filetype string `yaml:"filetype"`
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
