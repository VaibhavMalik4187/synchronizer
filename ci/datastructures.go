package main

type AutoGenerated struct {
	Name string `yaml:"name"`
	On   On     `yaml:"on"`
	Jobs Jobs   `yaml:"jobs"`
}
type PullRequest struct {
	Types       []string `yaml:"types"`
	Branches    []string `yaml:"branches"`
	PathsIgnore []string `yaml:"paths-ignore"`
}
type GOVERSION struct {
	Required bool   `yaml:"required"`
	Type     string `yaml:"type"`
}
type CGOENABLED struct {
	Required bool   `yaml:"required"`
	Type     string `yaml:"type"`
	Default  int    `yaml:"default"`
}
type BUILDPATH struct {
	Required bool   `yaml:"required"`
	Type     string `yaml:"type"`
	Default  string `yaml:"default"`
}
type UNITTESTSPATH struct {
	Required bool   `yaml:"required"`
	Type     string `yaml:"type"`
	Default  string `yaml:"default"`
}
type Inputs struct {
	GOVERSION     GOVERSION     `yaml:"GO_VERSION"`
	CGOENABLED    CGOENABLED    `yaml:"CGO_ENABLED"`
	BUILDPATH     BUILDPATH     `yaml:"BUILD_PATH"`
	UNITTESTSPATH UNITTESTSPATH `yaml:"UNIT_TESTS_PATH"`
}
type SNYKTOKEN struct {
	Required bool `yaml:"required"`
}
type GITGUARDIANAPIKEY struct {
	Required bool `yaml:"required"`
}
type Secrets struct {
	SNYKTOKEN         SNYKTOKEN         `yaml:"SNYK_TOKEN"`
	GITGUARDIANAPIKEY GITGUARDIANAPIKEY `yaml:"GITGUARDIAN_API_KEY"`
}
type WorkflowCall struct {
	Inputs  Inputs  `yaml:"inputs"`
	Secrets Secrets `yaml:"secrets"`
}
type On struct {
	PullRequest  PullRequest  `yaml:"pull_request"`
	WorkflowCall WorkflowCall `yaml:"workflow_call"`
}
type Permissions struct {
	PullRequests   string `yaml:"pull-requests"`
	SecurityEvents string `yaml:"security-events"`
}
type With struct {
	GOVERSION     string `yaml:"GO_VERSION"`
	GO111MODULE   string `yaml:"GO111MODULE"`
	CGOENABLED    string `yaml:"CGO_ENABLED"`
	UNITTESTSPATH string `yaml:"UNIT_TESTS_PATH"`
	BUILDPATH     string `yaml:"BUILD_PATH"`
}
type Test struct {
	Permissions Permissions `yaml:"permissions"`
	Uses        string      `yaml:"uses"`
	With        With        `yaml:"with"`
	Secrets     string      `yaml:"secrets"`
}
type Jobs struct {
	Test Test `yaml:"test"`
}
