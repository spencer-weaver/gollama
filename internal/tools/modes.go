package tools

// Mode defines a named set of tools available within that mode.
// Add new modes here to extend the framework.
type Mode struct {
	Name        string
	Description string
	Tools       []string
}

// BuiltinModes maps mode names to their tool sets.
// To add a new mode: register tools in DefaultRegistry(), then add an entry here.
var BuiltinModes = map[string]Mode{
	"research": {
		Name:        "research",
		Description: "Fetch URLs and read local files for research tasks",
		Tools:       []string{"fetch_url", "read_file", "find_files", "search_files"},
	},
	"analyze": {
		Name:        "analyze",
		Description: "Traverse and read a local codebase for architecture analysis",
		Tools:       []string{"list_dir", "read_file", "find_files", "search_files"},
	},
	"dev": {
		Name:        "dev",
		Description: "Full dev environment: read, write, search, and run commands",
		Tools:       []string{"list_dir", "read_file", "write_file", "find_files", "search_files", "run_command"},
	},
	"agent": {
		Name:        "agent",
		Description: "Voice agent: run commands, spawn background tasks, delegate to Claude",
		Tools:       []string{"run_command", "spawn_background", "ask_claude"},
	},
	"part-research": {
		Name:        "part-research",
		Description: "Research a component: search the web, fetch datasheets, write component IR",
		Tools:       []string{"search_web", "fetch_url", "write_file", "read_file"},
	},
	"ha": {
		Name:        "ha",
		Description: "Home Assistant agent: run shell commands and spawn background tasks for long-running actions",
		Tools:       []string{"run_command", "spawn_background"},
	},
}

// DefaultRegistry returns a Registry pre-populated with all built-in tools.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(NewFetchURLTool())
	r.Register(NewSearchWebTool())
	r.Register(NewReadFileTool())
	r.Register(NewListDirTool())
	r.Register(NewFindFilesTool())
	r.Register(NewSearchFilesTool())
	r.Register(NewWriteFileTool())
	r.Register(NewRunCommandTool())
	return r
}

// ToolsForMode returns the tool names for a given mode name.
// Returns nil if the mode is not recognized.
func ToolsForMode(mode string) []string {
	if m, ok := BuiltinModes[mode]; ok {
		return m.Tools
	}
	return nil
}
