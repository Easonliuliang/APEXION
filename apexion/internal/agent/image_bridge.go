package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/apexion-ai/apexion/internal/tui"
)

type imageBridgeTool struct {
	Server string
	Name   string
	Schema any
}

const imageBridgeAnalysisPrompt = "Please provide a comprehensive visual analysis of this image, including OCR text, layout, UI elements, entities, and important details."

// bridgeImagesViaMCP resolves image attachments through an MCP image tool
// (preferably understand_image) and returns text-only context for the model.
func (a *Agent) bridgeImagesViaMCP(ctx context.Context, userText string, images []tui.ImageAttachment) (string, bool, string) {
	if len(images) == 0 {
		return userText, false, "no images to bridge"
	}
	if a.mcpManager == nil {
		return userText, false, "MCP not configured"
	}

	tool, ok := selectImageBridgeTool(a.mcpManager.AllTools())
	if !ok {
		return userText, false, "no image-understanding MCP tool found"
	}

	a.io.SystemMessage(fmt.Sprintf("Image attached: using MCP tool %s/%s for image understanding.", tool.Server, tool.Name))

	type bridgeTask struct {
		cacheKey string
		img      tui.ImageAttachment
		indices  []int
	}
	type bridgeTaskResult struct {
		output string
	}

	analysisByIndex := make([]string, len(images))
	cacheHits := 0
	var tasks []bridgeTask
	taskByKey := make(map[string]int)

	for i, img := range images {
		key := imageBridgeCacheKey(img)
		if cached, ok := a.imageBridgeCacheGet(key); ok && strings.TrimSpace(cached) != "" {
			analysisByIndex[i] = cached
			cacheHits++
			continue
		}

		if idx, exists := taskByKey[key]; exists {
			tasks[idx].indices = append(tasks[idx].indices, i)
			continue
		}
		taskByKey[key] = len(tasks)
		tasks = append(tasks, bridgeTask{
			cacheKey: key,
			img:      img,
			indices:  []int{i},
		})
	}

	if cacheHits > 0 {
		a.io.SystemMessage(fmt.Sprintf("Image bridge cache hit: %d image(s) reused without MCP call.", cacheHits))
	}

	if len(tasks) > 0 {
		results := make([]bridgeTaskResult, len(tasks))
		toolFullName := fmt.Sprintf("mcp__%s__%s", tool.Server, tool.Name)
		maxParallel := len(tasks)
		if maxParallel > 4 {
			maxParallel = 4
		}
		sem := make(chan struct{}, maxParallel)
		var wg sync.WaitGroup

		for i := range tasks {
			wg.Add(1)
			go func(taskIdx int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				task := tasks[taskIdx]
				label := task.img.Label
				if strings.TrimSpace(label) == "" {
					label = fmt.Sprintf("image-%d", taskIdx+1)
				}
				toolID := fmt.Sprintf("imgbridge-%d", taskIdx+1)
				a.io.ToolStart(toolID, toolFullName, fmt.Sprintf("{\"image\":\"%s\"}", label))

				attempts := buildImageBridgeArgs(tool.Schema, imageBridgeAnalysisPrompt, task.img)
				var (
					output  string
					lastErr string
				)
				for _, args := range attempts {
					out, isError, err := a.mcpManager.CallTool(ctx, tool.Server, tool.Name, args)
					if err != nil {
						lastErr = err.Error()
						continue
					}
					if isError {
						lastErr = out
						continue
					}
					output = strings.TrimSpace(out)
					if output != "" {
						break
					}
					lastErr = "empty response from MCP tool"
				}

				if output == "" {
					if lastErr == "" {
						lastErr = "unknown MCP tool error"
					}
					a.io.ToolDone(toolID, toolFullName, "error: "+lastErr, true)
					return
				}

				results[taskIdx] = bridgeTaskResult{output: output}
				a.io.ToolDone(toolID, toolFullName, truncateBridgeOutput(output, 600), false)
			}(i)
		}
		wg.Wait()

		for i, task := range tasks {
			res := results[i]
			if strings.TrimSpace(res.output) == "" {
				continue
			}
			a.imageBridgeCachePut(task.cacheKey, res.output)
			for _, idx := range task.indices {
				analysisByIndex[idx] = res.output
			}
		}
	}

	var analyses []string
	for i, img := range images {
		if strings.TrimSpace(analysisByIndex[i]) == "" {
			continue
		}
		analyses = append(analyses, fmt.Sprintf("Image #%d (%s):\n%s", i+1, img.Label, analysisByIndex[i]))
	}

	if len(analyses) == 0 {
		return userText, false, "MCP image tool failed for all attachments"
	}

	augmented := strings.TrimSpace(userText)
	if augmented == "" {
		augmented = "Please analyze this image."
	}
	augmented += "\n\n[Image analysis by MCP understand_image]\n" + strings.Join(analyses, "\n\n")

	if a.eventLogger != nil {
		a.eventLogger.Log(EventType("image_mcp_bridge"), map[string]any{
			"tool":        fmt.Sprintf("%s/%s", tool.Server, tool.Name),
			"image_count": len(images),
			"success":     len(analyses),
			"cache_hits":  cacheHits,
		})
	}

	return augmented, true, ""
}

func imageBridgeCacheKey(img tui.ImageAttachment) string {
	sum := sha256.Sum256([]byte(img.MediaType + ":" + img.Data))
	return hex.EncodeToString(sum[:])
}

func (a *Agent) imageBridgeCacheGet(key string) (string, bool) {
	a.imageBridgeCacheMu.RLock()
	defer a.imageBridgeCacheMu.RUnlock()
	v, ok := a.imageBridgeCache[key]
	return v, ok
}

func (a *Agent) imageBridgeCachePut(key, value string) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return
	}
	a.imageBridgeCacheMu.Lock()
	defer a.imageBridgeCacheMu.Unlock()
	a.imageBridgeCache[key] = value
}

func truncateBridgeOutput(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func selectImageBridgeTool(all map[string][]*mcpsdk.Tool) (imageBridgeTool, bool) {
	var best imageBridgeTool
	bestScore := -1

	servers := make([]string, 0, len(all))
	for server := range all {
		servers = append(servers, server)
	}
	sort.Strings(servers)

	for _, server := range servers {
		for _, tool := range all[server] {
			score := scoreImageBridgeTool(server, tool.Name)
			if score > bestScore {
				bestScore = score
				best = imageBridgeTool{
					Server: server,
					Name:   tool.Name,
					Schema: tool.InputSchema,
				}
			}
		}
	}

	if bestScore < 0 {
		return imageBridgeTool{}, false
	}
	return best, true
}

func scoreImageBridgeTool(server, toolName string) int {
	name := strings.ToLower(strings.TrimSpace(toolName))
	srv := strings.ToLower(strings.TrimSpace(server))

	score := -1
	switch {
	case name == "understand_image":
		score = 100
	case strings.Contains(name, "understand") && strings.Contains(name, "image"):
		score = 90
	case strings.Contains(name, "image") && strings.Contains(name, "analy"):
		score = 85
	case strings.Contains(name, "vision"):
		score = 80
	case strings.Contains(name, "image"):
		score = 70
	}

	if score >= 0 && strings.Contains(srv, "minimax") {
		score += 10
	}
	return score
}

func buildImageBridgeArgs(schema any, prompt string, img tui.ImageAttachment) []map[string]any {
	if strings.TrimSpace(prompt) == "" {
		prompt = "Please describe this image in detail."
	}
	dataURI := fmt.Sprintf("data:%s;base64,%s", img.MediaType, img.Data)

	var candidates []map[string]any
	props, required := schemaPropsAndRequired(schema)
	if len(props) > 0 {
		args := make(map[string]any)
		for key := range props {
			if val, ok := valueForImageBridgeKey(key, prompt, img, dataURI); ok {
				args[key] = val
			}
		}
		for _, rk := range required {
			if _, exists := args[rk]; exists {
				continue
			}
			if val, ok := valueForImageBridgeKey(rk, prompt, img, dataURI); ok {
				args[rk] = val
			}
		}
		if len(args) > 0 {
			candidates = append(candidates, args)
		}
	}

	candidates = append(candidates,
		map[string]any{"prompt": prompt, "image_source": dataURI},
		map[string]any{"image_url": dataURI, "question": prompt},
		map[string]any{"url": dataURI, "question": prompt},
		map[string]any{"image": dataURI, "question": prompt},
		map[string]any{"image_base64": img.Data, "mime_type": img.MediaType, "question": prompt},
		map[string]any{"base64": img.Data, "media_type": img.MediaType, "question": prompt},
	)

	// Deduplicate candidates.
	seen := make(map[string]bool)
	uniq := make([]map[string]any, 0, len(candidates))
	for _, c := range candidates {
		raw, _ := json.Marshal(c)
		key := string(raw)
		if seen[key] {
			continue
		}
		seen[key] = true
		uniq = append(uniq, c)
	}
	return uniq
}

func schemaPropsAndRequired(schema any) (map[string]any, []string) {
	m, ok := schema.(map[string]any)
	if !ok {
		return nil, nil
	}

	props, _ := m["properties"].(map[string]any)

	var required []string
	if req, ok := m["required"].([]any); ok {
		for _, v := range req {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				required = append(required, s)
			}
		}
	}
	return props, required
}

func valueForImageBridgeKey(key, prompt string, img tui.ImageAttachment, dataURI string) (any, bool) {
	normalized := normalizeBridgeKey(key)
	switch normalized {
	case "imagesource", "source":
		return dataURI, true
	case "imageurl", "url", "imageuri", "uri", "resourceurl":
		return dataURI, true
	case "image", "inputimage", "imageinput", "resource":
		return dataURI, true
	case "imagebase64", "base64", "imagedata", "data", "imagecontent":
		return img.Data, true
	case "mediatype", "mimetype", "imagetype", "contenttype":
		return img.MediaType, true
	case "filename", "name", "label", "file":
		return img.Label, true
	case "question", "query", "prompt", "instruction", "text", "message":
		return prompt, true
	case "resourcemode", "mode":
		return "url", true
	}
	return nil, false
}

func normalizeBridgeKey(key string) string {
	k := strings.ToLower(strings.TrimSpace(key))
	k = strings.ReplaceAll(k, "_", "")
	k = strings.ReplaceAll(k, "-", "")
	return k
}
