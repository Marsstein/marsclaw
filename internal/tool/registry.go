package tool

import t "github.com/marsstein/liteclaw/internal/types"

// Registry holds available tools and their executors.
type Registry struct {
	defs      []t.ToolDef
	executors map[string]t.ToolExecutor
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{executors: make(map[string]t.ToolExecutor)}
}

// Register adds a tool with its executor.
func (r *Registry) Register(def t.ToolDef, exec t.ToolExecutor) {
	r.defs = append(r.defs, def)
	r.executors[def.Name] = exec
}

// Defs returns all registered tool definitions.
func (r *Registry) Defs() []t.ToolDef { return r.defs }

// Executors returns the executor map.
func (r *Registry) Executors() map[string]t.ToolExecutor { return r.executors }

// DefaultRegistry returns a registry with all built-in tools.
func DefaultRegistry(workDir string) *Registry {
	r := NewRegistry()
	r.Register(ReadFileDef(), ReadFileTool{})
	r.Register(WriteFileDef(), WriteFileTool{})
	r.Register(EditFileDef(), EditFileTool{})
	r.Register(ShellDef(), ShellTool{WorkDir: workDir})
	r.Register(ListFilesDef(), ListFilesTool{})
	r.Register(SearchDef(), SearchTool{})
	r.Register(GitDef(), GitTool{WorkDir: workDir})
	return r
}
