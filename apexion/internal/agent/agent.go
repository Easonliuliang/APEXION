package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/apexion-ai/apexion/internal/config"
	"github.com/apexion-ai/apexion/internal/mcp"
	"github.com/apexion-ai/apexion/internal/provider"
	"github.com/apexion-ai/apexion/internal/repomap"
	"github.com/apexion-ai/apexion/internal/session"
	"github.com/apexion-ai/apexion/internal/tools"
	"github.com/apexion-ai/apexion/internal/tui"
)

// ProviderFactory creates a Provider from a config. Used for /provider hot-swap.
type ProviderFactory func(cfg *config.Config) (provider.Provider, error)

// Agent orchestrates the interactive loop between user, LLM, and tools.
type Agent struct {
	provider        provider.Provider
	executor        *tools.Executor
	config          *config.Config
	session         *session.Session
	store           session.Store
	memoryStore     session.MemoryStore
	mcpManager      *mcp.Manager
	basePrompt      string // system prompt without identity suffix
	systemPrompt    string
	io              tui.IO
	summarizer      session.Summarizer
	providerFactory ProviderFactory
	customCommands  map[string]*CustomCommand
	planMode        bool
	rules           []Rule
	skills          []SkillInfo
	hookManager     *tools.HookManager
	eventLogger     *EventLogger
	checkpointMgr   *CheckpointManager
	costTracker     *CostTracker
	repoMap         *repomap.RepoMap
	bgManager       *BackgroundManager
	promptVariant   string // "full" or "lite"
	architectNext   bool   // next prompt uses architect mode
	architectAuto   bool   // architect auto-execute
	// imageBridgeCache stores MCP image analysis by image fingerprint so
	// repeated prompts on the same attachment can skip extra MCP calls.
	imageBridgeCache   map[string]string
	imageBridgeCacheMu sync.RWMutex
	toolHealth         map[string]*toolHealthState
	toolHealthMu       sync.RWMutex
	firstStepAllowed   map[string]bool
	firstStepPrimary   []string
	firstStepRequire   bool
	firstStepRetry     bool
	firstStepAllowedMu sync.RWMutex
}

// New creates a new Agent with the given IO implementation.
// Pass tui.NewPlainIO() for plain terminal mode.
func New(p provider.Provider, exec *tools.Executor, cfg *config.Config, ui tui.IO, store session.Store) *Agent {
	return NewWithSession(p, exec, cfg, ui, store, session.New())
}

// NewWithSession creates a new Agent with an existing session.
func NewWithSession(p provider.Provider, exec *tools.Executor, cfg *config.Config, ui tui.IO, store session.Store, sess *session.Session) *Agent {
	cwd, _ := os.Getwd()

	// Load modular system prompt from embedded defaults + user overrides.
	variant := modelPromptVariant(cfg.Provider)
	base := loadSystemPrompt(cwd, variant)
	if cfg.SystemPrompt != "" {
		base = cfg.SystemPrompt // full override from config
	}

	// Append project context from APEXION.md / .apexion/context.md
	if ctx := loadProjectContext(cwd); ctx != "" {
		base += ctx
	}

	// Initialize cost tracker with optional user pricing overrides.
	var costOverrides map[string]ModelPricing
	if len(cfg.CostPricing) > 0 {
		costOverrides = make(map[string]ModelPricing, len(cfg.CostPricing))
		for model, entry := range cfg.CostPricing {
			costOverrides[model] = ModelPricing{
				InputPerMillion:  entry.InputPerMillion,
				OutputPerMillion: entry.OutputPerMillion,
			}
		}
	}

	a := &Agent{
		provider:         p,
		executor:         exec,
		config:           cfg,
		session:          sess,
		store:            store,
		basePrompt:       base,
		promptVariant:    variant,
		io:               ui,
		summarizer:       &session.LLMSummarizer{Provider: p},
		customCommands:   loadCustomCommands(cwd),
		rules:            loadRules(cwd),
		skills:           loadSkills(cwd),
		costTracker:      NewCostTracker(costOverrides),
		imageBridgeCache: make(map[string]string),
		toolHealth:       make(map[string]*toolHealthState),
		firstStepAllowed: make(map[string]bool),
	}

	// Initialize repo map (async build in background).
	if !cfg.RepoMap.Disabled {
		maxTokens := cfg.RepoMap.MaxTokens
		if maxTokens <= 0 {
			maxTokens = 4096
		}
		a.repoMap = repomap.New(cwd, maxTokens, cfg.RepoMap.Exclude)
		go a.repoMap.Build()
	}

	a.rebuildSystemPrompt()
	a.wireTaskTool()
	return a
}

// SetProviderFactory sets the factory function for /provider hot-swap.
func (a *Agent) SetProviderFactory(f ProviderFactory) {
	a.providerFactory = f
}

// SetMemoryStore injects the cross-session memory store and rebuilds the system prompt
// to include relevant memories.
func (a *Agent) SetMemoryStore(ms session.MemoryStore) {
	a.memoryStore = ms
	a.rebuildSystemPrompt()
}

// SetMCPManager injects the MCP manager for /mcp command and status display.
func (a *Agent) SetMCPManager(m *mcp.Manager) {
	a.mcpManager = m
}

// SetHookManager injects the hook manager for lifecycle hooks and /hooks command.
func (a *Agent) SetHookManager(hm *tools.HookManager) {
	a.hookManager = hm
}

// rebuildSystemPrompt appends a dynamic identity suffix and persistent memories to basePrompt.
// Call after changing provider, model, or memory store.
func (a *Agent) rebuildSystemPrompt() {
	model := a.config.Model
	if model == "" {
		model = a.provider.DefaultModel()
	}
	a.systemPrompt = a.basePrompt + fmt.Sprintf(
		"\n\nYou are powered by %s (provider: %s, model: %s). "+
			"When asked about your identity, state these facts. Never claim to be a different model.",
		a.config.Provider, a.config.Provider, model)

	// Inject persistent memories if available.
	if a.memoryStore != nil {
		cwd, _ := os.Getwd()
		projectTag := "project:" + filepath.Base(cwd)
		if mem := a.memoryStore.LoadForPrompt(projectTag, 2048); mem != "" {
			a.systemPrompt += "\n\n" + mem
		}
	}

	// Inject always-active rules.
	for _, r := range a.rules {
		if len(r.PathPatterns) == 0 {
			a.systemPrompt += "\n\n<rule name=\"" + r.Name + "\">\n" + r.Content + "\n</rule>"
		}
	}

	// Inject repo map if available and built.
	if a.repoMap != nil && a.repoMap.IsBuilt() {
		if mapContent := a.repoMap.Render(0); mapContent != "" {
			a.systemPrompt += "\n\n<repo_map>\n" + mapContent + "</repo_map>"
		}
	}

	// List available skills so the LLM knows what it can load.
	if len(a.skills) > 0 {
		a.systemPrompt += "\n\nAvailable project skills (load with read_file tool when you need detailed knowledge):"
		for _, s := range a.skills {
			desc := s.Desc
			if desc == "" {
				desc = s.Name
			}
			a.systemPrompt += "\n- " + s.Path + " — " + desc
		}
	}
}

// imageInputSupport returns whether the current provider/model should accept
// image attachments, plus a short reason used in user-facing diagnostics.
func (a *Agent) imageInputSupport() (supported bool, reason string, model string) {
	model = a.config.Model
	if model == "" {
		model = a.provider.DefaultModel()
	}

	pc := a.config.GetProviderConfig(a.config.Provider)
	decision := provider.DetectImageSupportWithConfig(
		a.config.Provider,
		model,
		pc.ImageInput,
		pc.ImageModelsAllow,
		pc.ImageModelsDeny,
	)
	if !decision.Supported && decision.Confident {
		return false, decision.Reason, model
	}
	return true, decision.Reason, model
}

// Run starts the interactive REPL loop.
func (a *Agent) Run(ctx context.Context) error {
	// Initialize event logger.
	if el, err := NewEventLogger(a.session.ID); err == nil {
		a.eventLogger = el
		defer a.eventLogger.Close()
		a.eventLogger.Log(EventSessionStart, map[string]string{
			"session_id": a.session.ID,
		})
	}

	// Initialize checkpoint manager.
	a.checkpointMgr = NewCheckpointManager(10)

	// Initialize background agent manager.
	a.bgManager = NewBackgroundManager(4, a.io)
	a.wireBGLauncher()

	// Fire session_start hooks.
	if a.hookManager != nil {
		a.hookManager.RunLifecycleHooks(ctx, tools.HookSessionStart, map[string]string{
			"session_id": a.session.ID,
		})
	}

	for {
		input, err := a.io.ReadInput()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		// Collect any images the user attached via drag-drop or clipboard paste.
		// Must be done before the empty-input check, since the user may have
		// typed only an image path (which becomes empty after path extraction).
		var images []tui.ImageAttachment
		if ii, ok := a.io.(tui.ImageInput); ok {
			images = ii.PendingImages()
		}

		if input == "" && len(images) == 0 {
			continue
		}

		// Slash commands are intercepted before sending to LLM.
		if strings.HasPrefix(input, "/") {
			handled, shouldQuit := a.handleSlashCommand(ctx, input)
			if shouldQuit {
				return nil
			}
			if handled {
				continue
			}
		}

		// Check if architect mode is pending for this prompt.
		if a.architectNext {
			a.architectNext = false
			a.io.UserMessage(input)
			am := NewArchitectMode(a, a.config.Architect.ArchitectModel, a.config.Architect.CoderModel, a.architectAuto)
			if err := am.Run(ctx, input); err != nil {
				a.io.Error(err.Error())
			}
			a.architectAuto = false
			continue
		}

		// Enforce a model capability gate for image inputs.
		if len(images) > 0 {
			supported, reason, model := a.imageInputSupport()
			isMiniMaxProvider := strings.EqualFold(a.config.Provider, "minimax")
			bridged := false

			// MiniMax OpenAI-compatible chat is text-only: bridge attachments through
			// MCP understand_image. Also attempt this bridge for any explicitly
			// unsupported model/provider combination.
			if isMiniMaxProvider || !supported {
				if bridgedInput, ok, _ := a.bridgeImagesViaMCP(ctx, input, images); ok {
					input = bridgedInput
					images = nil // send text-only prompt augmented with MCP analysis
					bridged = true
				}
			}

			if len(images) > 0 && !bridged {
				if isMiniMaxProvider {
					a.io.SystemMessage("MiniMax OpenAI-compatible chat API does not accept image input directly. Configure MiniMax MCP (understand_image) and retry.")
					continue
				}
				if !supported {
					a.io.SystemMessage(fmt.Sprintf(
						"Model %s does not support image inputs (%s). Switch model/provider and resend.",
						model, reason,
					))
					if a.eventLogger != nil {
						a.eventLogger.Log("image_blocked", map[string]any{
							"provider": a.config.Provider,
							"model":    model,
							"reason":   reason,
						})
					}
					continue
				}
			}
		}

		// Build user message with text and optional images.
		if input == "" {
			input = "Please look at this image."
		}
		a.io.UserMessage(input)
		contents := []provider.Content{{
			Type: provider.ContentTypeText,
			Text: input,
		}}
		for _, img := range images {
			contents = append(contents, provider.Content{
				Type:           provider.ContentTypeImage,
				ImageData:      img.Data,
				ImageMediaType: img.MediaType,
			})
		}
		a.session.AddMessage(provider.Message{
			Role:    provider.RoleUser,
			Content: contents,
		})

		if a.eventLogger != nil {
			evt := map[string]any{"text": input}
			if len(images) > 0 {
				evt["image_count"] = len(images)
			}
			a.eventLogger.Log(EventUserMessage, evt)
		}

		if err := a.runAgentLoop(ctx); err != nil {
			if ctx.Err() != nil {
				a.io.SystemMessage("\nInterrupted.")
				_ = a.store.Save(a.session)
				return ctx.Err()
			}
			a.io.Error(err.Error())
		}

		// Fire notification hooks after each agent turn completes.
		if a.hookManager != nil {
			a.hookManager.RunLifecycleHooks(ctx, tools.HookNotification, map[string]string{
				"session_id": a.session.ID,
			})
		}
	}

	// Wait for background agents before exiting.
	if a.bgManager != nil && a.bgManager.RunningCount() > 0 {
		a.io.SystemMessage("Waiting for background agents to complete...")
		a.bgManager.WaitAll(ctx)
	}

	// Show file change summary on exit if any files were modified.
	if changes := a.executor.FileTracker().Summary(); changes != "" {
		a.io.SystemMessage("\n--- Session file changes ---\n" + changes)
	}

	// Fire session_stop hooks.
	if a.hookManager != nil {
		a.hookManager.RunLifecycleHooks(ctx, tools.HookSessionStop, map[string]string{
			"session_id": a.session.ID,
		})
	}

	// Auto-extract memories from the conversation.
	if a.memoryStore != nil && len(a.session.Messages) > 5 {
		extractor := NewAutoMemoryExtractor(a.provider, a.memoryStore, a.config.SubAgentModel)
		if n, err := extractor.Extract(ctx, a.session.Messages, a.session.ID); err == nil && n > 0 {
			a.io.SystemMessage(fmt.Sprintf("Auto-extracted %d memories from this session.", n))
		}
	}

	// Log session end.
	if a.eventLogger != nil {
		a.eventLogger.Log(EventSessionEnd, map[string]string{
			"session_id":  a.session.ID,
			"tokens_used": fmt.Sprintf("%d", a.session.TokensUsed),
		})
	}

	_ = a.store.Save(a.session)
	return nil
}

// RunOnce executes a single prompt and exits (non-interactive mode).
func (a *Agent) RunOnce(ctx context.Context, prompt string) error {
	a.io.UserMessage(prompt)
	a.session.AddMessage(provider.Message{
		Role: provider.RoleUser,
		Content: []provider.Content{{
			Type: provider.ContentTypeText,
			Text: prompt,
		}},
	})
	return a.runAgentLoop(ctx)
}
