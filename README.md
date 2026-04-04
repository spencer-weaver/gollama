# gollama

A software development assistant for the PRAXIS ecosystem. Understands your codebase, talks through it, and builds when you're ready. Uses tools when they help. Reasons directly when they don't.

One model. One conversation per project. gobin-powered tool execution.

---

## What It Is

gollama is a project-scoped AI development assistant backed by any OpenAI-compatible model endpoint. You point it at a project directory and it knows where it is — what the project does, its current state, its architecture decisions, and where you left off last session.

It is not a blind task executor. It will talk through a problem, push back on an approach, help you think before it builds. When you're ready to build, it uses gobin tools to read files, write code, run tests, and verify results. When the work changes the project's shape, it updates the project's README and commits both the README and its own context file via git.

As the PRAXIS tool ecosystem grows, gollama grows with it. Install a new tool with gobin and gollama knows about it on next start.

---

## Project Context

gollama maintains two files per project, both committed to git:

### `README.md`

The human-facing project description. Generated through a conversation with gollama when a project is first initialized. Updated when the project's scope, architecture, or direction changes — gollama will ask whether to update it when the conversation warrants. Commit history of `README.md` becomes a readable record of how the project has evolved.

### `.gollama/context.md`

gollama's own structured scratchpad. Records current state, open questions, recent decisions, and anything else the model needs to resume effectively. Not intended for human editing but fully readable. Also committed to git — diff it to see what gollama's understanding of the project has changed between sessions.

Neither file is touched silently. gollama proposes changes, you confirm, it commits with a descriptive message.

---

## Core Loop

```
gollama --project <dir>
    ↓
load README.md + .gollama/context.md
    ↓
inject project context + gobin tool list into system prompt
    ↓
human input
    ↓
model (any OpenAI-compatible endpoint)
    ↓
tool call? ──yes──→ gobin agent run <tool> <command> [args]
    ↓                       ↓
    no              result injected into context
    ↓                       ↓
response ←──────────────────┘
    ↓
verify tool results before responding
    ↓
context changed? → propose README/context update → commit if confirmed
    ↓
human sees output / continues conversation
```

Tool results are verified before the model responds. If a tool call fails, the model is told so and must reconsider. The model does not report success on a failed tool call.

---

## Initialization

When `gollama --project <dir>` is run against a directory with no `README.md`:

```
$ gollama --project ./goread

No README.md found. Let's set up this project.
What does goread do?

> File reading tool. Reads files in full, by line range, or by pattern search.
  Part of the PRAXIS ecosystem — used by humans and agents via gobin.

What's the current state — greenfield, in progress, or existing code to understand?

> Greenfield. Just the standard PRAXIS layout so far.

[gollama generates README.md and .gollama/context.md]
[proposes git commit: "Initial project scope"]
[commits on confirmation]

Ready. What do you want to work on?
```

---

## Configuration

### `config/gollama.json`

```json
{
  "endpoint": "https://api.anthropic.com/v1",
  "model": "claude-sonnet-4-20250514",
  "api_key_env": "ANTHROPIC_API_KEY",
  "max_tokens": 8192,
  "system_prompt_base": "You are a software development assistant. You help developers understand, discuss, and build software projects. Think before building. Talk through problems when that's what's needed. Use tools when they're the right answer. When you use a tool, verify the result before responding. Do not report success unless the tool confirmed it.",
  "gobin_path": "~/.praxis/bin",
  "max_history_turns": 50
}
```

Any OpenAI-compatible endpoint works. Point `endpoint` at Ollama (`http://localhost:11434/v1`), a local LM Studio instance, the Anthropic API, or any other compatible provider.

---

## Tool Discovery

At startup, gollama calls `gobin agent list` and injects the result into the system prompt alongside the project context. The model knows what tools exist, what they do, and how to call them before the first human message arrives.

When the model returns a tool call, gollama:
1. Parses the tool name, command, and arguments
2. Calls `gobin agent run <tool> <command> [args]`
3. Reads the JSON result
4. Injects the result into the conversation context
5. Continues the model loop

If `ok` is false in the result, the model is explicitly told the call failed and given the error. It must address the failure before proceeding.

---

## Human Interface

```
gollama --project <dir>          Start or resume session for a project
gollama --project <dir> --fresh  Start a new session, keep context files
gollama --model <model>          Override model for this session
gollama --endpoint <url>         Override endpoint for this session
gollama config                   Print current configuration
gollama version                  Print gollama version
```

### Example Session

```
$ gollama --project ./gobin

Resuming session for gobin.
Last session: added agent interface design to context.

gollama > I'm thinking the registry should scan the bin directory lazily
          rather than loading everything at startup.

[model discusses tradeoffs — startup time vs. per-call overhead,
 relevance to gobin's use pattern, whether it matters at this scale]

gollama > Yeah you're right, it doesn't matter yet. Let's implement
          the eager load and revisit if it becomes an issue.

[model agrees, notes the decision in context]
[proposes .gollama/context.md update]
[commits: "Note: eager registry load chosen, revisit if perf warrants"]

gollama > Go ahead and scaffold the registry package.

[model reads project layout via goread]
[creates internal/registry/registry.go and loader.go via goedit]
[runs go build via goshell to verify it compiles]
[reports result]

gollama > The scope of gobin has grown since the README was written,
          it should mention the agent interface now.

[model proposes README edits]
[commits: "README: document agent interface" on confirmation]
```

---

## Project Layout

```
gollama/
├── assets/
├── cmd/
│   └── gollama/
│       └── main.go
├── config/
│   ├── gollama.json
│   └── gobin.json
├── internal/
│   ├── commands/
│   │   ├── chat.go
│   │   └── config.go
│   ├── config/
│   │   ├── config.go
│   │   └── gollama.go
│   ├── agent/
│   │   ├── agent.go          ← core conversation loop
│   │   ├── toolcall.go       ← parse and dispatch tool calls via gobin
│   │   └── verify.go         ← result verification before model response
│   ├── context/
│   │   ├── context.go        ← load/save README.md and .gollama/context.md
│   │   └── git.go            ← git commit helpers
│   ├── history/
│   │   └── history.go        ← in-session conversation history
│   └── llm/
│       ├── client.go         ← OpenAI-compatible HTTP client
│       └── stream.go         ← streaming response handling
└── go.mod
```

---

## Design Notes

- gollama does not execute tools directly. gobin handles execution. gollama handles conversation and dispatch.
- Project context is the primary memory mechanism. Session history is secondary — it covers the current conversation, context files cover everything across sessions.
- README.md is the human record. `.gollama/context.md` is gollama's record. Both live in git. Neither is modified silently.
- Git commit messages are written by the model and should be meaningful. They form a lightweight project journal.
- Model is swappable. The loop does not care whether it's talking to Claude, a local Ollama model, or anything else that speaks the OpenAI chat completions API.
- Verification is enforced. The loop checks tool results before continuing. A model that reports success on a failed call gets corrected, not trusted.
- Voice, HA integration, and other domain-specific features are separate tools registered via gobin — not part of gollama's core.