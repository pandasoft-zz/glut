package executor

type Executor struct{}

type JobOutput struct {
	ExitStatus int
	Stdout     string
	Stderr     string
}
