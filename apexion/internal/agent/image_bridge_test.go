package agent

import (
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/apexion-ai/apexion/internal/tui"
)

func TestSelectImageBridgeTool_PrefersUnderstandImage(t *testing.T) {
	all := map[string][]*mcpsdk.Tool{
		"generic": {
			{Name: "vision_analyze"},
		},
		"minimax": {
			{Name: "understand_image"},
		},
	}

	got, ok := selectImageBridgeTool(all)
	if !ok {
		t.Fatal("expected to find an image bridge tool")
	}
	if got.Server != "minimax" || got.Name != "understand_image" {
		t.Fatalf("unexpected tool selection: %+v", got)
	}
}

func TestBuildImageBridgeArgs_UsesSchemaFields(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"image_url": map[string]any{"type": "string"},
			"question":  map[string]any{"type": "string"},
		},
		"required": []any{"image_url"},
	}
	img := tui.ImageAttachment{
		Data:      "QUJD",
		MediaType: "image/png",
		Label:     "demo.png",
	}

	args := buildImageBridgeArgs(schema, "请描述图片", img)
	if len(args) == 0 {
		t.Fatal("expected at least one args candidate")
	}

	first := args[0]
	url, _ := first["image_url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,QUJD") {
		t.Fatalf("unexpected image_url: %q", url)
	}
	if first["question"] != "请描述图片" {
		t.Fatalf("unexpected question: %+v", first["question"])
	}
}

func TestBuildImageBridgeArgs_UsesMiniMaxSchemaFields(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"image_source": map[string]any{"type": "string"},
			"prompt":       map[string]any{"type": "string"},
		},
		"required": []any{"prompt", "image_source"},
	}
	img := tui.ImageAttachment{
		Data:      "QUJD",
		MediaType: "image/png",
		Label:     "demo.png",
	}

	args := buildImageBridgeArgs(schema, "请描述图片", img)
	if len(args) == 0 {
		t.Fatal("expected at least one args candidate")
	}

	first := args[0]
	src, _ := first["image_source"].(string)
	if !strings.HasPrefix(src, "data:image/png;base64,QUJD") {
		t.Fatalf("unexpected image_source: %q", src)
	}
	if first["prompt"] != "请描述图片" {
		t.Fatalf("unexpected prompt: %+v", first["prompt"])
	}
}

func TestBuildImageBridgeArgs_FallbackIncludesImageURL(t *testing.T) {
	img := tui.ImageAttachment{
		Data:      "AAAA",
		MediaType: "image/jpeg",
		Label:     "x.jpg",
	}

	args := buildImageBridgeArgs(nil, "", img)
	if len(args) == 0 {
		t.Fatal("expected fallback candidates")
	}

	found := false
	for _, a := range args {
		if u, ok := a["image_url"].(string); ok && strings.HasPrefix(u, "data:image/jpeg;base64,AAAA") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected image_url fallback in candidates: %+v", args)
	}
}

func TestImageBridgeCacheKey_StableAndDistinct(t *testing.T) {
	a := tui.ImageAttachment{Data: "AAAA", MediaType: "image/png"}
	b := tui.ImageAttachment{Data: "AAAA", MediaType: "image/png"}
	c := tui.ImageAttachment{Data: "BBBB", MediaType: "image/png"}

	ka := imageBridgeCacheKey(a)
	kb := imageBridgeCacheKey(b)
	kc := imageBridgeCacheKey(c)

	if ka != kb {
		t.Fatalf("expected same key for same image payload: %q vs %q", ka, kb)
	}
	if ka == kc {
		t.Fatalf("expected different key for different image payload: %q", ka)
	}
}
